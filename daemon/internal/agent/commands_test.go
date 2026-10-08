package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type commandAdapter struct {
	Adapter
	catalog CommandCatalog
	query   CommandQuery
}

func (a *commandAdapter) Commands(_ context.Context, q CommandQuery) (CommandCatalog, error) {
	a.query = q
	return a.catalog, nil
}

func TestCommandsResolveNativeInputAndNeverSendControlsAsPrompts(t *testing.T) {
	a := &commandAdapter{catalog: CommandCatalog{Commands: []Command{
		{Name: "review", Aliases: []string{"check"}, Kind: "prompt", Input: "$review", SkillPath: "/repo/.agents/skills/review/SKILL.md", AcceptsArguments: true},
		{Name: "permissions", Kind: "settings"}, {Name: "compact", Kind: "compact"},
	}}}
	in := TurnInput{Message: "untrusted native path or prompt", CWD: "/repo", Config: map[string]string{"model": "selected"}, Env: map[string]string{"PRIVATE_KEY": "secret"},
		Options: TurnOptions{Command: &CommandInvocation{Name: "check", Arguments: "  latest changes\nwith tests  "}, Attachments: []Attachment{{Path: "/repo/image.png"}}}}
	out, err := PrepareCommand(t.Context(), a, in)
	if err != nil || out.Message != "$review latest changes\nwith tests" || out.Command.SkillPath != "/repo/.agents/skills/review/SKILL.md" || len(out.Options.Attachments) != 1 {
		t.Fatalf("native dispatch lost context: %+v, %v", out, err)
	}
	if a.query.CWD != in.CWD || a.query.Config["model"] != "selected" || a.query.Env["PRIVATE_KEY"] != "secret" {
		t.Fatal("discovery must use the same project and authentication as the run")
	}
	for _, name := range []string{"permissions", "compact", "missing", "../../evil", "_internal"} {
		in.Options.Command = &CommandInvocation{Name: name}
		if _, err := PrepareCommand(t.Context(), a, in); err == nil {
			t.Fatalf("accepted unsupported native dispatch %q", name)
		}
	}
	data, err := json.Marshal(a.catalog)
	if err != nil || strings.Contains(string(data), "SKILL.md") || strings.Contains(string(data), "$review") {
		t.Fatal("server-only native invocation details leaked to the client")
	}
}

func TestCommandDiscoverySharesWorkAcrossCancellationAndRefresh(t *testing.T) {
	var cache CommandCache
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	fetch := func(ctx context.Context) (CommandCatalog, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return CommandCatalog{Commands: []Command{{Name: "native"}}}, nil
		case <-ctx.Done():
			return CommandCatalog{}, ctx.Err()
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	first := make(chan error, 1)
	go func() { _, err := cache.Get(ctx, "project", false, fetch); first <- err }()
	<-started
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled waiter: %v", err)
	}
	var waiters sync.WaitGroup
	for range 12 {
		waiters.Go(func() {
			catalog, err := cache.Get(t.Context(), "project", false, fetch)
			if err != nil || len(catalog.Commands) != 1 {
				t.Errorf("shared result: %+v %v", catalog, err)
			}
		})
	}
	close(release)
	waiters.Wait()
	if calls.Load() != 1 {
		t.Fatalf("navigation cancellation started %d child processes", calls.Load())
	}
	if _, err := cache.Get(t.Context(), "project", true, fetch); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("explicit refresh didn't refresh native metadata")
	}
}

func TestCommandCacheIsolatesWorkspaceCredentialsAndConfig(t *testing.T) {
	q := CommandQuery{CWD: "/a", Config: map[string]string{"model": "one"}, Env: map[string]string{"TOKEN": "private-secret"}}
	key := CommandCacheKey("mindwire-test-missing-cli", q)
	if strings.Contains(key, "private-secret") {
		t.Fatal("cache key contains a credential")
	}
	q.Refresh = true
	if CommandCacheKey("mindwire-test-missing-cli", q) != key {
		t.Fatal("refresh must join pending discovery for the same identity")
	}
	q.CWD = "/b"
	if CommandCacheKey("mindwire-test-missing-cli", q) == key {
		t.Fatal("project skills crossed directories")
	}
	q.CWD = "/a"
	q.Env["TOKEN"] = "another-account"
	if CommandCacheKey("mindwire-test-missing-cli", q) == key {
		t.Fatal("command list crossed accounts")
	}
	q.Env["TOKEN"] = "private-secret"
	q.Config["model"] = "two"
	if CommandCacheKey("mindwire-test-missing-cli", q) == key {
		t.Fatal("changed settings kept stale discovery")
	}
}

func TestExplicitCommandHistoryReconcilesNativeSkillSpellingOnly(t *testing.T) {
	stamp := "2026-10-08T00:00:00Z"
	native := []Message{{ID: "native", Role: "user", Text: "$review latest", CreatedAt: stamp}}
	recorded := []Message{{ID: "sent", Role: "user", Text: "/review latest", CreatedAt: stamp,
		Command: &CommandInvocation{Name: "review", Arguments: "latest"}}}
	merged := MergeRecordedHistory(native, recorded)
	if len(merged) != 1 || merged[0].ID != "native" || merged[0].Text != "/review latest" || merged[0].Command == nil {
		t.Fatalf("selected skill became duplicate history: %+v", merged)
	}
	recorded[0].Command = nil
	if len(MergeRecordedHistory(native, recorded)) != 2 {
		t.Fatal("unmarked user prose must never be rewritten as a command")
	}
}
