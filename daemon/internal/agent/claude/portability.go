package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

var _ agent.SessionPorter = adapter{}

func portableProjectSlug(cwd string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, cwd)
}

func (adapter) ExportSession(ctx context.Context, cwd, sid string) (agent.NativeArchive, error) {
	out := agent.NativeArchive{Version: 1, Harness: "claude-code", SessionID: sid}
	if !agent.ValidNativeSessionID(sid) {
		return out, fmt.Errorf("invalid Claude session ID")
	}
	base := configBase()
	primary := findTranscript(base, cwd, sid)
	if primary == "" {
		return out, fmt.Errorf("Claude native session %s is missing; synchronization stopped", sid)
	}
	actual, err := sessionDirectory(primary)
	if err != nil {
		return out, err
	}
	wanted, err := workspacepath.Canonical(cwd)
	if err != nil {
		return out, err
	}
	if actual != "" && actual != wanted {
		return out, fmt.Errorf("Claude session belongs to another directory; refusing to modify that conversation")
	}
	out.Files = append(out.Files, agent.NativeFile{Name: "project/" + sid + ".jsonl", Path: primary, JSONLines: true})
	for _, item := range [][2]string{{filepath.Join(filepath.Dir(primary), sid), "project/" + sid}, {filepath.Join(filepath.Dir(primary), "memory"), "project/memory"}, {filepath.Join(base, "file-history", sid), "file-history/" + sid}, {filepath.Join(base, "tasks", sid), "tasks/" + sid}} {
		files, err := agent.ArchiveTree(ctx, item[0], item[1])
		if err != nil {
			return out, err
		}
		out.Files = append(out.Files, files...)
	}
	// Claude's saved plan is referenced by slug in the selected transcript.
	f, err := os.Open(primary)
	if err != nil {
		return out, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4096), 32<<20)
	slugs := map[string]bool{}
	for sc.Scan() {
		var row struct {
			Slug string `json:"slug"`
		}
		if json.Unmarshal(sc.Bytes(), &row) == nil && agent.ValidNativeSessionID(row.Slug) {
			slugs[row.Slug] = true
		}
	}
	if err = sc.Err(); err != nil {
		return out, err
	}
	for slug := range slugs {
		files, err := agent.ArchiveTree(ctx, filepath.Join(base, "plans", slug+".md"), "plans/"+slug+".md")
		if err != nil {
			return out, err
		}
		out.Files = append(out.Files, files...)
	}
	return out, nil
}

func (adapter) ImportSessionPath(cwd, sid, name string) (string, error) {
	if !agent.ValidNativeSessionID(sid) || !agent.SafeArchiveName(name) {
		return "", fmt.Errorf("invalid Claude archive path")
	}
	base := configBase()
	if strings.HasPrefix(name, "project/") {
		rel := strings.TrimPrefix(name, "project/")
		if rel != sid+".jsonl" && !strings.HasPrefix(rel, sid+"/") && !strings.HasPrefix(rel, "memory/") {
			return "", fmt.Errorf("unrelated Claude session in archive")
		}
		// Keep a pre-existing native location (including historical slug spellings).
		dir := filepath.Join(base, "projects", portableProjectSlug(cwd))
		if p := findTranscript(base, cwd, sid); p != "" {
			dir = filepath.Dir(p)
		}
		return filepath.Join(dir, filepath.FromSlash(rel)), nil
	}
	if strings.HasPrefix(name, "file-history/"+sid+"/") || strings.HasPrefix(name, "tasks/"+sid+"/") ||
		(strings.HasPrefix(name, "plans/") && strings.HasSuffix(name, ".md") && strings.Count(name, "/") == 1) {
		return filepath.Join(base, filepath.FromSlash(name)), nil
	}
	return "", fmt.Errorf("unsupported Claude archive path")
}
