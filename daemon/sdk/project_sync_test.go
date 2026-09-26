package mindwire

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSDKProjectSyncResumesPartialRelayAndPreservesBothCopies(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	makeClient := func() (*Client, string) {
		root := t.TempDir()
		project := filepath.Join(root, "project")
		if err := os.MkdirAll(project, 0700); err != nil {
			t.Fatal(err)
		}
		c, err := New(Options{CWD: root, StatePath: filepath.Join(root, "private", "state.json")})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		return c, project
	}
	x, xpath := makeClient()
	y, ypath := makeClient()
	data := []byte(strings.Repeat("project checkpoint\n", 40000))
	if err := os.WriteFile(filepath.Join(xpath, "code.txt"), data, 0644); err != nil {
		t.Fatal(err)
	}
	_, err := x.Workspace.Projects.Put("source", WorkspaceProject{Record: WorkspaceRecord{ID: "source"}, Name: "Example", Path: xpath}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	_, err = x.Workspace.Sync.SwitchTo(ctx, y.Workspace.Sync, ProjectSyncRequest{ID: "first", ProjectID: "source", Path: ypath}, func(int64) { cancel() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected interrupted relay, got %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(xpath, "code.txt")); err != nil || !bytes.Equal(got, data) {
		t.Fatal("interrupted relay changed source", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := x.Workspace.Sync.SwitchTo(ctx, y.Workspace.Sync, ProjectSyncRequest{ID: "first", ProjectID: "source", Path: ypath}, nil)
	if err != nil || result.Status != "succeeded" {
		t.Fatalf("resume %+v: %v", result, err)
	}
	if got, err := os.ReadFile(filepath.Join(ypath, "code.txt")); err != nil || !bytes.Equal(got, data) {
		t.Fatal("resume lost code", err)
	}
	if err = os.WriteFile(filepath.Join(xpath, "X-only"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(ypath, "Y-only"), []byte("bring back"), 0600); err != nil {
		t.Fatal(err)
	}
	back, err := y.Workspace.Sync.SwitchTo(ctx, x.Workspace.Sync, ProjectSyncRequest{ID: "back", ProjectID: result.ResultProjectID}, nil)
	if err != nil || back.Status != "succeeded" {
		t.Fatalf("round trip %+v: %v", back, err)
	}
	for _, file := range []string{"X-only", "Y-only", "code.txt"} {
		if _, err = os.Stat(filepath.Join(xpath, file)); err != nil {
			t.Fatal("lost", file, err)
		}
	}
	if _, err = os.Stat(filepath.Join(ypath, "Y-only")); err != nil {
		t.Fatal("return sync changed its source")
	}
	// An accepted operation can be reconciled without reading the source at all.
	if err = y.Close(); err != nil {
		t.Fatal(err)
	}
	replayed, err := y.Workspace.Sync.SwitchTo(ctx, x.Workspace.Sync, ProjectSyncRequest{ID: "back", ProjectID: result.ResultProjectID}, nil)
	if err != nil || replayed.ResultCheckpointID != back.ResultCheckpointID {
		t.Fatal("destination replay depended on closed source", err)
	}
}
