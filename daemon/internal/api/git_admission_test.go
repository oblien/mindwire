package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/workspaceexec"
)

type gitWorkingAdapter struct {
	resolveFakeAdapter
	started chan struct{}
}

func (f *gitWorkingAdapter) RunStream(ctx context.Context, _ agent.TurnInput, _ agent.Emit) (agent.TurnResult, error) {
	close(f.started)
	<-ctx.Done()
	return agent.TurnResult{}, ctx.Err()
}

func TestGitHTTPStageAndUnstageDuringOrdinaryWork(t *testing.T) {
	for _, work := range []string{"command", "terminal", "agent"} {
		t.Run(work, func(t *testing.T) {
			fake := &gitWorkingAdapter{resolveFakeAdapter: resolveFakeAdapter{id: "git-stage-active-agent"}, started: make(chan struct{})}
			if work == "agent" {
				agent.Register(fake)
			}
			h, a, dir := gitHTTPFixture(t)
			path := filepath.Join(dir, "tracked")
			if err := os.WriteFile(path, []byte("base\n"), 0600); err != nil {
				t.Fatal(err)
			}
			apiGit(t, "-C", dir, "add", "tracked")
			apiGit(t, "-C", dir, "-c", "user.email=fixture@example.invalid", "commit", "-m", "Base")
			if err := os.WriteFile(path, []byte("working changes\n"), 0600); err != nil {
				t.Fatal(err)
			}
			switch work {
			case "command":
				ctx, cancel := context.WithCancel(context.Background())
				started, done := make(chan struct{}, 1), make(chan struct{})
				go func() {
					defer close(done)
					_, _ = a.execution.Exec(ctx, workspaceexec.ExecRequest{
						Argv: []string{"sh", "-c", "printf ready; exec sleep 60"}, Directory: dir,
					}, func(_ string, _ []byte) {
						select {
						case started <- struct{}{}:
						default:
						}
					})
				}()
				t.Cleanup(func() { cancel(); <-done })
				select {
				case <-started:
				case <-time.After(5 * time.Second):
					t.Fatal("command did not start")
				}
			case "terminal":
				t.Setenv("SHELL", "/bin/sh")
				terminal, err := a.execution.Terminals.Open(workspaceexec.TerminalRequest{
					ID: "git-stage-terminal", Directory: dir, Columns: 80, Rows: 24,
				})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = a.execution.Terminals.Remove(terminal.ID) })
			case "agent":
				adapter, ok := a.sup.Resolve(fake.ID())
				if !ok {
					t.Fatal("could not resolve test adapter")
				}
				run, err := a.sup.StartTurnChecked(adapter, orchestrator.StartTurnInput{ChatID: "chat", Message: "Work", CWD: dir})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { a.sup.Cancel(run.ID); a.sup.Wait() })
				select {
				case <-fake.started:
				case <-time.After(5 * time.Second):
					t.Fatal("agent did not start")
				}
			}
			if !a.BusyPath(dir) {
				t.Fatal("fixture has no active work")
			}
			for _, action := range []string{"stage", "unstage"} {
				body := `{"id":"` + action + `","action":"` + action + `","paths":["tracked"]}`
				got := serve(t, h, "POST", "/workspace/projects/project/git/operations", body)
				var operation registry.GitOperation
				if got.Code != 202 || json.Unmarshal(got.Body.Bytes(), &operation) != nil {
					t.Fatalf("%s during %s: %d %s", action, work, got.Code, got.Body.String())
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				result, err := a.gitJobs.Wait(ctx, operation.ID)
				cancel()
				if err != nil || result.Status != "succeeded" {
					t.Fatalf("%s result: %+v %v", action, result, err)
				}
				staged := apiGit(t, "-C", dir, "diff", "--cached", "--name-only")
				if (action == "stage" && staged != "tracked") || (action == "unstage" && staged != "") {
					t.Fatalf("%s index: %q", action, staged)
				}
				data, err := os.ReadFile(path)
				if err != nil || string(data) != "working changes\n" {
					t.Fatalf("%s changed working files: %q %v", action, data, err)
				}
				if !a.BusyPath(dir) {
					t.Fatalf("%s interrupted active work", action)
				}
			}
			got := serve(t, h, "POST", "/workspace/projects/project/git/operations", `{"id":"discard","action":"discard","paths":["tracked"]}`)
			if got.Code != 409 || strings.Contains(got.Body.String(), "record changed") {
				t.Fatalf("discard must retain a clear active-work guard: %d %s", got.Code, got.Body.String())
			}
		})
	}
}

func TestGitHTTPIndexActionsRespectProjectSyncReservation(t *testing.T) {
	h, a, dir := gitHTTPFixture(t)
	if err := a.registry.ReserveSync("sync", dir); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"stage", "unstage"} {
		body := `{"id":"` + action + `","action":"` + action + `","paths":["tracked"]}`
		got := serve(t, h, "POST", "/workspace/projects/project/git/operations", body)
		if got.Code != 409 || !strings.Contains(got.Body.String(), "synchronization") {
			t.Fatalf("%s during sync: %d %s", action, got.Code, got.Body.String())
		}
	}
}
