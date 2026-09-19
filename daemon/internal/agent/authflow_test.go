package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAuthFlowScopesInputCancellationAndCompletion(t *testing.T) {
	f := NewAuthFlow("login")
	defer f.Cancel()
	var code string
	f.SetInput(func(input string) error { code = input; return nil })
	f.Update(AuthState{Status: "needs_input", URL: "https://example.test/login", Fields: []Field{{Key: AuthCodeKey, Required: true}}})
	id := f.State().FlowID
	if _, err := f.Step(map[string]string{AuthFlowIDKey: "old", AuthCodeKey: "secret"}); err == nil || code != "" {
		t.Fatal("a stale attempt must not submit a code")
	}
	if _, err := f.Step(map[string]string{AuthActionKey: "cancel"}); err == nil || !f.Active() {
		t.Fatal("unscoped cancel must not terminate another client's flow")
	}
	if _, err := f.Step(map[string]string{AuthFlowIDKey: id, AuthCodeKey: "one\ntwo"}); err == nil || code != "" {
		t.Fatal("multiple input lines must not be forwarded")
	}
	state, err := f.Step(map[string]string{AuthFlowIDKey: id, AuthCodeKey: " code "})
	if err != nil || code != "code" || state.Status != "pending" || len(state.Fields) != 0 {
		t.Fatalf("code submission: %+v %v", state, err)
	}
	state, err = f.Step(map[string]string{AuthFlowIDKey: id, AuthActionKey: "cancel"})
	if err != nil || state.Status != "error" || state.URL != "" || f.Context().Err() != context.Canceled {
		t.Fatalf("cancel: %+v %v", state, err)
	}
	committed := false
	f.Complete(func() error { committed = true; return nil })
	f.Update(AuthState{Status: "pending", URL: "https://late.test"})
	if committed || f.State().Status != "error" || f.State().URL != "" {
		t.Fatal("late native output must not revive or commit a canceled login")
	}
}

func TestAuthFlowDoesNotReportCompletionWhenPersistenceFails(t *testing.T) {
	f := NewAuthFlow("login")
	f.Update(AuthState{Status: "pending", Code: "secret-code"})
	f.Complete(func() error { return errors.New("disk full") })
	st := f.State()
	if st.Status != "error" || st.Code != "" || !strings.Contains(st.Message, "save") {
		t.Fatalf("failed persistence: %+v", st)
	}
}
