package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

var _ agent.SessionLister = adapter{}

// Claude's resume list lives in its project session files; there is no CLI
// command for listing all completed foreground sessions as JSON. Read the same
// metadata without starting Claude or copying transcripts into Mindwire.
func (adapter) ListSessions(ctx context.Context, cwd string) ([]agent.NativeSession, error) {
	canonical, err := workspacepath.Canonical(cwd)
	if err != nil {
		return nil, err
	}
	base := configBase()
	if base == "" {
		return nil, nil
	}
	// Current Claude escapes all non-alphanumeric path characters. Include the
	// historical separator-only spelling and the caller's symlink spelling too.
	directories := map[string]bool{}
	for _, path := range []string{cwd, canonical} {
		modern := strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
				return r
			}
			return '-'
		}, path)
		for _, slug := range []string{modern, strings.NewReplacer("/", "-", "\\", "-").Replace(path)} {
			directories[filepath.Join(base, "projects", slug)] = true
		}
	}
	// A conversation may have been started through another symlink spelling
	// (including macOS /var versus /private/var). Resolve the native directory's
	// own cwd metadata; do not infer membership merely from its lossy slug.
	projectDirs, err := os.ReadDir(filepath.Join(base, "projects"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, dir := range projectDirs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path := filepath.Join(base, "projects", dir.Name())
		if !dir.IsDir() || directories[path] {
			continue
		}
		files, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if file.IsDir() || !strings.HasSuffix(file.Name(), ".jsonl") || strings.HasPrefix(file.Name(), "agent-") {
				continue
			}
			actual, err := sessionDirectory(filepath.Join(path, file.Name()))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if actual == "" {
				continue
			}
			if actual == canonical {
				directories[path] = true
			}
			break
		}
	}
	byID := map[string]agent.NativeSession{}
	for directory := range directories {
		files, err := os.ReadDir(directory)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if file.IsDir() || !strings.HasSuffix(file.Name(), ".jsonl") || strings.HasPrefix(file.Name(), "agent-") {
				continue
			}
			session, err := readSessionMetadata(filepath.Join(directory, file.Name()), canonical)
			if os.IsNotExist(err) {
				continue
			} // native removal during refresh
			if err != nil {
				return nil, err
			}
			if session.ID != "" {
				if old, ok := byID[session.ID]; !ok || session.UpdatedAt > old.UpdatedAt {
					byID[session.ID] = session
				}
			}
		}
	}
	out := make([]agent.NativeSession, 0, len(byID))
	for _, row := range byID {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt == out[j].UpdatedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out, nil
}

// Only enough of one native file to identify a project directory. The ordinary
// metadata reader is then used for every session in matching directories.
func sessionDirectory(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4096), 16<<20)
	read := 0
	for sc.Scan() {
		var row struct {
			CWD string `json:"cwd"`
		}
		if json.Unmarshal(sc.Bytes(), &row) == nil && row.CWD != "" {
			return workspacepath.Canonical(row.CWD)
		}
		read += len(sc.Bytes()) + 1
		if read >= 64<<10 {
			break
		}
	}
	return "", sc.Err()
}

func readSessionMetadata(path, cwd string) (agent.NativeSession, error) {
	var result agent.NativeSession
	f, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() {
		return result, nil
	}
	id := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if !agent.ValidNativeSessionID(id) {
		return result, nil
	}
	firstPrompt, title, customTitle, actual, created := "", "", "", "", ""
	sidechain := false
	consume := func(line []byte, header bool) {
		var row struct {
			Type             string          `json:"type"`
			SessionID        string          `json:"sessionId"`
			CWD              string          `json:"cwd"`
			Timestamp        string          `json:"timestamp"`
			IsSidechain      bool            `json:"isSidechain"`
			IsMeta           bool            `json:"isMeta"`
			IsCompactSummary bool            `json:"isCompactSummary"`
			AITitle          string          `json:"aiTitle"`
			CustomTitle      string          `json:"customTitle"`
			Message          json.RawMessage `json:"message"`
		}
		if json.Unmarshal(line, &row) != nil || row.SessionID != "" && row.SessionID != id {
			return
		}
		if row.Type == "ai-title" && row.AITitle != "" {
			title = row.AITitle
		}
		if row.Type == "custom-title" && row.CustomTitle != "" {
			customTitle = row.CustomTitle
		}
		if !header {
			return
		}
		if actual == "" && row.CWD != "" {
			actual, _ = workspacepath.Canonical(row.CWD)
		}
		if row.IsSidechain {
			sidechain = true
		}
		if created == "" && (row.Type == "user" || row.Type == "assistant") {
			if _, err := time.Parse(time.RFC3339Nano, row.Timestamp); err == nil {
				created = row.Timestamp
			}
		}
		if row.Type == "user" && !row.IsMeta && !row.IsCompactSummary && firstPrompt == "" {
			firstPrompt = agent.SessionTitle(messageText(row.Message))
		}
	}
	// Metadata only: bound the prefix and suffix, rather than parsing every tool
	// result in a large conversation just to draw a row. A single native JSON line
	// still gets the same 16 MiB limit as the existing transcript reader.
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 16<<20)
	read := 0
	for scanner.Scan() {
		consume(scanner.Bytes(), true)
		read += len(scanner.Bytes()) + 1
		if sidechain || actual != "" && actual != cwd {
			return result, nil
		}
		if read >= 1<<20 {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return result, err
	}
	if actual != cwd || firstPrompt == "" {
		return result, nil
	}
	if info.Size() > int64(read) {
		start := max(int64(0), info.Size()-(128<<10))
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			return result, err
		}
		r := bufio.NewReader(f)
		if start > 0 {
			_, _ = r.ReadString('\n')
		}
		tail := bufio.NewScanner(r)
		tail.Buffer(make([]byte, 4096), 16<<20)
		for tail.Scan() {
			consume(tail.Bytes(), false)
		}
		if err := tail.Err(); err != nil && !errors.Is(err, io.EOF) {
			return result, err
		}
	}
	if created == "" {
		created = info.ModTime().UTC().Format(time.RFC3339Nano)
	}
	return agent.NativeSession{ID: id, CWD: cwd, Title: agent.SessionTitle(agent.FirstNonEmpty(customTitle, title, firstPrompt)),
		CreatedAt: created, UpdatedAt: info.ModTime().UTC().Format(time.RFC3339Nano)}, nil
}
