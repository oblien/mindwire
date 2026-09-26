package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

var _ agent.SessionPorter = adapter{}

func (adapter) ExportSession(ctx context.Context, cwd, sid string) (agent.NativeArchive, error) {
	out := agent.NativeArchive{Version: 1, Harness: "codex", SessionID: sid}
	if !agent.ValidNativeSessionID(sid) {
		return out, fmt.Errorf("invalid Codex session ID")
	}
	base := configBase()
	primary := findRollout(base, sid)
	if primary == "" {
		return out, fmt.Errorf("Codex native session %s is missing; synchronization stopped", sid)
	}
	type rollout struct{ path, id, parent, cwd string }
	var inventory []rollout
	for _, root := range []string{"sessions", "archived_sessions"} {
		err := filepath.WalkDir(filepath.Join(base, root), func(p string, d fs.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".jsonl") {
				return nil
			}
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			defer f.Close()
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 4096), 32<<20)
			if !sc.Scan() {
				return sc.Err()
			}
			var row struct {
				Type    string `json:"type"`
				Payload struct {
					ID     string          `json:"id"`
					CWD    string          `json:"cwd"`
					Source json.RawMessage `json:"source"`
				} `json:"payload"`
			}
			if json.Unmarshal(sc.Bytes(), &row) != nil || row.Type != "session_meta" {
				return nil
			}
			parent := ""
			var source any
			_ = json.Unmarshal(row.Payload.Source, &source)
			var walk func(any)
			walk = func(v any) {
				switch x := v.(type) {
				case map[string]any:
					for k, v := range x {
						if k == "parent_thread_id" || k == "parentThreadId" {
							parent, _ = v.(string)
						} else {
							walk(v)
						}
					}
				case []any:
					for _, v := range x {
						walk(v)
					}
				}
			}
			walk(source)
			inventory = append(inventory, rollout{p, row.Payload.ID, parent, row.Payload.CWD})
			return nil
		})
		if err != nil {
			return out, err
		}
	}
	selected := map[string]bool{sid: true}
	for changed := true; changed; {
		changed = false
		for _, r := range inventory {
			if selected[r.parent] && !selected[r.id] {
				selected[r.id] = true
				changed = true
			}
		}
	}
	found := false
	for _, r := range inventory {
		if !selected[r.id] {
			continue
		}
		if r.id == sid {
			actual, err := workspacepath.Canonical(r.cwd)
			if err != nil {
				return out, err
			}
			wanted, err := workspacepath.Canonical(cwd)
			if err != nil {
				return out, err
			}
			if actual != wanted {
				return out, fmt.Errorf("Codex session belongs to another directory; refusing to modify that conversation")
			}
			found = true
		}
		rel, err := filepath.Rel(base, r.path)
		if err != nil {
			return out, err
		}
		out.Files = append(out.Files, agent.NativeFile{Name: filepath.ToSlash(rel), Path: r.path, JSONLines: true})
		name := "memory/" + r.id + ".json"
		memory, err := (adapter{}).ReadSessionData(ctx, cwd, sid, name)
		if err != nil {
			return out, err
		}
		if memory != nil {
			out.Files = append(out.Files, agent.NativeFile{Name: name, Data: memory, Resource: true, JSONLines: true})
		}
	}
	if !found {
		return out, fmt.Errorf("unsupported Codex transcript format")
	}
	return out, nil
}

func (adapter) ImportSessionPath(cwd, sid, name string) (string, error) {
	if !agent.ValidNativeSessionID(sid) || !agent.SafeArchiveName(name) ||
		!(strings.HasPrefix(name, "sessions/") || strings.HasPrefix(name, "archived_sessions/")) || !strings.HasSuffix(name, ".jsonl") || !strings.HasPrefix(filepath.Base(name), "rollout-") {
		return "", fmt.Errorf("unsupported Codex archive path")
	}
	return filepath.Join(configBase(), filepath.FromSlash(name)), nil
}
