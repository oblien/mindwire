package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

type sessionRPCFixture struct {
	calls []map[string]any
	pages []string
	err   error
}

func (f *sessionRPCFixture) call(_ context.Context, method string, params any, result any) error {
	if method != "thread/list" {
		return errors.New("listing attempted to create or resume a turn")
	}
	f.calls = append(f.calls, params.(map[string]any))
	if f.err != nil {
		return f.err
	}
	if len(f.pages) == 0 {
		return errors.New("unexpected extra page")
	}
	page := f.pages[0]
	f.pages = f.pages[1:]
	return json.Unmarshal([]byte(page), result)
}

func TestNativeSessionListUsesCodexPaginationAndAllProviders(t *testing.T) {
	cwd, _ := workspacepath.Canonical(t.TempDir())
	page := func(data []map[string]any, cursor string) string {
		b, _ := json.Marshal(map[string]any{"data": data, "nextCursor": cursor})
		return string(b)
	}
	f := &sessionRPCFixture{pages: []string{
		page([]map[string]any{
			{"id": "thread-one", "sessionId": "native-one", "cwd": cwd, "name": "Native title", "preview": "first prompt", "createdAt": 100, "updatedAt": 200},
			{"id": "sibling", "cwd": cwd + "-sibling", "preview": "Wrong folder"},
			{"id": "ephemeral", "cwd": cwd, "ephemeral": true},
		}, "next"),
		page([]map[string]any{{"id": "two", "cwd": cwd, "preview": "Second\nquestion", "updatedAt": 300}}, ""),
	}}
	rows, err := listNativeSessions(context.Background(), f, cwd, cwd)
	if err != nil || len(rows) != 2 {
		t.Fatalf("list: %+v %v", rows, err)
	}
	if rows[0].ID != "native-one" || rows[0].Aliases[0] != "thread-one" || rows[0].Title != "Native title" || rows[1].Title != "Second question" {
		t.Fatalf("native identities/titles: %+v", rows)
	}
	if rows[0].UpdatedAt != "1970-01-01T00:03:20Z" {
		t.Fatal(rows[0].UpdatedAt)
	}
	if len(f.calls) != 2 || f.calls[1]["cursor"] != "next" {
		t.Fatalf("pagination: %+v", f.calls)
	}
	for _, call := range f.calls {
		providers, ok := call["modelProviders"].([]string)
		if !ok || len(providers) != 0 || call["cwd"] != cwd || call["archived"] != false {
			t.Fatalf("listing must include custom providers: %+v", call)
		}
	}
}

func TestNativeSessionListRejectsPartialOrLoopingInventories(t *testing.T) {
	cwd, _ := workspacepath.Canonical(t.TempDir())
	for _, f := range []*sessionRPCFixture{
		{err: errors.New("offline")},
		{pages: []string{`{"data":[],"nextCursor":"repeat"}`, `{"data":[],"nextCursor":"repeat"}`}},
	} {
		if rows, err := listNativeSessions(context.Background(), f, cwd, cwd); err == nil || rows != nil {
			t.Fatalf("partial list must not become authoritative: %+v %v", rows, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &sessionRPCFixture{}
	if _, err := listNativeSessions(ctx, f, cwd, cwd); !errors.Is(err, context.Canceled) || len(f.calls) != 0 {
		t.Fatal(err)
	}
}

func TestNativeSessionListDoesNotInstallOrCreateFreshHomes(t *testing.T) {
	home := filepath.Join(t.TempDir(), "not-created")
	t.Setenv("CODEX_HOME", home)
	if rows, err := (adapter{}).ListSessions(context.Background(), t.TempDir()); err != nil || len(rows) != 0 {
		t.Fatalf("fresh home: %+v %v", rows, err)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatal("listing created a native home")
	}
}

func TestNativeSessionListSmoke(t *testing.T) {
	cwd := os.Getenv("MINDWIRE_TEST_NATIVE_SESSION_CWD")
	if cwd == "" {
		t.Skip("explicit read-only native CLI check")
	}
	rows, err := (adapter{}).ListSessions(context.Background(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("native Codex conversations discovered: %d", len(rows))
}
