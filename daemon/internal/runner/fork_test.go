package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

type forkAdapter struct {
	*stubAdapter
	fn func(context.Context, agent.TurnInput, agent.Emit) (agent.TurnResult, error)
}

type noForkAdapter struct{ forkAdapter }

func (noForkAdapter) Capabilities() agent.Capabilities { return agent.Capabilities{} }

func TestLegacyPendingForkCannotResumeThroughAnUnsupportedHarness(t *testing.T) {
	st, _ := session.Open(filepath.Join(t.TempDir(), "state.json"))
	_ = st.SetSession("stub", "source", "native-source")
	_ = st.ForkChat("source", "branch")
	ad := noForkAdapter{forkAdapter{stubAdapter: &stubAdapter{}, fn: func(context.Context, agent.TurnInput, agent.Emit) (agent.TurnResult, error) {
		t.Fatal("unsupported adapter must not receive the source session")
		return agent.TurnResult{}, nil
	}}}
	r := New(st, ad, stubAuth{}, namespacedCreds{vals: map[string]string{}}, stream.New(), "/work")
	res, _ := r.RunTurn(context.Background(), Turn{ChatID: "branch", Message: "edit", RunID: "run"})
	if !res.IsError || st.PendingFork("stub", "branch") == nil || st.Session("stub", "source") != "native-source" {
		t.Fatal("unsupported fork lost its safety marker or changed the source")
	}
}

func (a forkAdapter) RunStream(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	return a.fn(ctx, in, emit)
}

func TestForkRetryNeverResumesTheSourceInPlace(t *testing.T) {
	for _, fresh := range []bool{false, true} {
		t.Run(map[bool]string{true: "first prompt", false: "historical prompt"}[fresh], func(t *testing.T) {
			st, _ := session.Open(filepath.Join(t.TempDir(), "state.json"))
			_ = st.SetSession("stub", "source", "native-source")
			point := agent.ForkPoint{BeforeMessageID: "selected", ResumeAt: "previous", Fresh: fresh}
			_ = st.ForkChat("source", "branch", session.ForkOptions{Agent: "stub", Point: point})
			calls := 0
			ad := forkAdapter{stubAdapter: &stubAdapter{}}
			ad.fn = func(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
				calls++
				if in.Options.SessionID != "" || in.Options.ContinueLatest || in.Options.ForkOnResume == fresh || in.Fork == nil || *in.Fork != point {
					t.Fatal("client flags overrode the fork plan")
				}
				if fresh && in.SessionID != "" || !fresh && in.SessionID != "native-source" {
					t.Fatal("wrong resume anchor")
				}
				if calls == 1 {
					return agent.TurnResult{}, errors.New("failed before initialization")
				}
				emit(agent.Event{Type: agent.EventSession, SessionID: "native-branch"})
				if st.Session("stub", "branch") != "native-branch" || st.PendingFork("stub", "branch") != nil {
					t.Fatal("new session was not saved before execution")
				}
				return agent.TurnResult{SessionID: "native-branch", Text: "provider unavailable", IsError: true}, nil
			}
			r := New(st, ad, stubAuth{}, namespacedCreds{vals: map[string]string{}}, stream.New(), "/work")
			turn := Turn{ChatID: "branch", RunID: "one", Message: "edited", Options: agent.TurnOptions{SessionID: "override-source", ContinueLatest: true}}
			res, _ := r.RunTurn(context.Background(), turn)
			if !res.IsError || st.PendingFork("stub", "branch") == nil {
				t.Fatal("failed start consumed fork")
			}
			res, _ = r.RunTurn(context.Background(), turn)
			if !res.IsError || st.PendingFork("stub", "branch") != nil || st.Session("stub", "source") != "native-source" {
				t.Fatal("provider failure lost the independent branch")
			}
		})
	}
}

func TestForkRejectsAnAdapterReturningTheOriginalSession(t *testing.T) {
	st, _ := session.Open(filepath.Join(t.TempDir(), "state.json"))
	_ = st.SetSession("stub", "source", "native-source")
	_ = st.ForkChat("source", "branch")
	ad := forkAdapter{stubAdapter: &stubAdapter{}, fn: func(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
		emit(agent.Event{Type: agent.EventSession, SessionID: "native-source"})
		if ctx.Err() == nil {
			t.Fatal("unsafe resume was not cancelled")
		}
		return agent.TurnResult{Text: "wrong", SessionID: "native-source"}, nil
	}}
	r := New(st, ad, stubAuth{}, namespacedCreds{vals: map[string]string{}}, stream.New(), "/work")
	res, _ := r.RunTurn(context.Background(), Turn{ChatID: "branch", Message: "edit", RunID: "run"})
	if !res.IsError || st.PendingFork("stub", "branch") == nil {
		t.Fatal("unsafe fork was committed")
	}
}

func TestForkPersistenceFailureAbortsBeforePromptAndStaysAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, _ := session.Open(path)
	_ = st.SetSession("stub", "source", "native-source")
	_ = st.ForkChat("source", "branch")
	if err := os.Mkdir(path+".tmp", 0700); err != nil {
		t.Fatal(err)
	}
	ad := forkAdapter{stubAdapter: &stubAdapter{}, fn: func(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
		emit(agent.Event{Type: agent.EventSession, SessionID: "native-branch"})
		if ctx.Err() == nil {
			t.Fatal("the provider could run before the new native identity was saved")
		}
		return agent.TurnResult{SessionID: "native-branch", Cancelled: true}, nil
	}}
	r := New(st, ad, stubAuth{}, namespacedCreds{vals: map[string]string{}}, stream.New(), "/work")
	res, _ := r.RunTurn(context.Background(), Turn{ChatID: "branch", Message: "edit", RunID: "run"})
	if !res.IsError || res.Cancelled || !strings.Contains(res.Text, "Could not save") {
		t.Fatalf("save failure was hidden as a successful stop: %+v", res)
	}
	reopened, err := session.Open(path)
	if err != nil || reopened.PendingFork("stub", "branch") == nil || reopened.Session("stub", "branch") != "native-source" {
		t.Fatal("failed save disarmed the fork after a restart")
	}
}
