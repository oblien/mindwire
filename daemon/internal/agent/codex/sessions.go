package codex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

var _ agent.SessionLister = adapter{}

// ListSessions proxies Codex's own paginated thread/list. In particular, an
// empty modelProviders filter includes native subscription AND custom-provider
// conversations. No thread/start or thread/resume is sent while browsing.
func (adapter) ListSessions(ctx context.Context, cwd string) ([]agent.NativeSession, error) {
	root := configBase()
	if root == "" {
		return nil, nil
	}
	if _, err := os.Stat(filepath.Join(root, "sessions")); os.IsNotExist(err) {
		return nil, nil // fresh/missing CLI: browsing must never install it
	}
	canonical, err := workspacepath.Canonical(cwd)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	client, err := startMetadataRPC(ctx, "codex app-server")
	if err != nil {
		return nil, fmt.Errorf("could not read Codex conversations: %w", err)
	}
	defer client.close()
	return listNativeSessions(ctx, client, cwd, canonical)
}

type sessionRPC interface {
	call(context.Context, string, any, any) error
}

func listNativeSessions(ctx context.Context, client sessionRPC, cwd, canonical string) ([]agent.NativeSession, error) {
	paths := []string{canonical}
	if cwd != canonical {
		paths = append(paths, cwd)
	}
	out := []agent.NativeSession{}
	seen := map[string]bool{}
	for _, path := range paths {
		cursor := ""
		cursors := map[string]bool{}
		for {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			params := map[string]any{"cwd": path, "limit": 50, "sortKey": "updated_at", "archived": false,
				"sourceKinds": []string{"cli", "vscode", "exec", "appServer"}, "modelProviders": []string{}}
			if cursor != "" {
				params["cursor"] = cursor
			}
			var page struct {
				Data []struct {
					ID        string `json:"id"`
					SessionID string `json:"sessionId"`
					CWD       string `json:"cwd"`
					Name      string `json:"name"`
					Preview   string `json:"preview"`
					CreatedAt int64  `json:"createdAt"`
					UpdatedAt int64  `json:"updatedAt"`
					Ephemeral bool   `json:"ephemeral"`
				} `json:"data"`
				NextCursor string `json:"nextCursor"`
			}
			if err := client.call(ctx, "thread/list", params, &page); err != nil {
				return nil, err // partial pages are not an authoritative empty list
			}
			for _, row := range page.Data {
				id := agent.FirstNonEmpty(row.SessionID, row.ID)
				actual, err := workspacepath.Canonical(row.CWD)
				if err != nil || actual != canonical || row.Ephemeral || !agent.ValidNativeSessionID(id) || seen[id] {
					continue
				}
				seen[id] = true
				created, updated := row.CreatedAt, row.UpdatedAt
				if created <= 0 {
					created = updated
				}
				if updated < created {
					updated = created
				}
				out = append(out, agent.NativeSession{ID: id, CWD: canonical,
					Title:     agent.SessionTitle(agent.FirstNonEmpty(row.Name, row.Preview, "Codex conversation")),
					CreatedAt: time.Unix(created, 0).UTC().Format(time.RFC3339Nano),
					UpdatedAt: time.Unix(updated, 0).UTC().Format(time.RFC3339Nano), Aliases: []string{row.ID}})
			}
			if page.NextCursor == "" {
				break
			}
			if cursors[page.NextCursor] {
				return nil, fmt.Errorf("Codex repeated its conversation page cursor")
			}
			cursors[page.NextCursor], cursor = true, page.NextCursor
		}
	}
	return out, nil
}
