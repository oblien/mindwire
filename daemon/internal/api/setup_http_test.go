package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/setup"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

type setupHTTPAdapter struct {
	resolveFakeAdapter
	steps []agent.Step
}

func (f setupHTTPAdapter) InstallSteps() []agent.Step { return f.steps }
func (f setupHTTPAdapter) RunStream(ctx context.Context, _ agent.TurnInput, _ agent.Emit) (agent.TurnResult, error) {
	<-ctx.Done()
	return agent.TurnResult{}, ctx.Err()
}

func TestSetupHTTPClientsShareLiveJobAndAtomicTurnAdmission(t *testing.T) {
	dir := t.TempDir()
	release, installed := filepath.Join(dir, "release"), filepath.Join(dir, "installed")
	quote := func(path string) string { return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'" }
	ad := setupHTTPAdapter{resolveFakeAdapter: resolveFakeAdapter{id: "setup-http"}, steps: []agent.Step{{
		Name: "cli", Install: "touch " + quote(installed),
		Check: "test -f " + quote(installed) + " || exit 1; while [ ! -f " + quote(release) + " ]; do sleep 0.01; done",
	}}}
	agent.Register(ad)
	store, err := session.Open(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	hub := stream.New()
	sup := orchestrator.New(store, hub, notify.Fanout(nil), dir, ad.ID())
	first, second := New(store, hub, sup), New(store, hub, sup)
	defer first.Close()
	defer second.Close()
	ag, _ := sup.Resolve(ad.ID())
	t.Cleanup(func() {
		_ = os.WriteFile(release, nil, 0600)
		deadline := time.Now().Add(5 * time.Second)
		for sup.SetupStatus(ag).Running && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
	})
	request := func(handler http.HandlerFunc, method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path+"?agent="+ad.ID(), strings.NewReader(body))
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}
	if w := request(first.setup, "POST", "/setup", ""); w.Code != http.StatusAccepted {
		t.Fatalf("start setup: %d %s", w.Code, w.Body)
	}
	deadline := time.Now().Add(5 * time.Second)
	var status setup.Status
	for time.Now().Before(deadline) {
		w := request(second.setupStatus, "GET", "/setup", "")
		if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
			t.Fatal(err)
		}
		if status.Stage == "verifying" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !status.Running || status.OK || status.Stage != "verifying" || status.Current != "cli" {
		t.Fatalf("second client must see live verification: %+v", status)
	}
	w := request(second.update, "POST", "/update", "")
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusAccepted || status.Operation != "setup" || status.Stage != "verifying" {
		t.Fatalf("second client must join setup: %d %+v", w.Code, status)
	}
	if w := request(second.turn, "POST", "/turns", `{"chatId":"chat","message":"hello"}`); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "setup") {
		t.Fatalf("turn while installing: %d %s", w.Code, w.Body)
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for sup.SetupStatus(ag).Running && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !sup.SetupStatus(ag).OK {
		t.Fatal("verification did not complete")
	}
	w = request(second.turn, "POST", "/turns", `{"chatId":"chat","message":"hello"}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("ready turn: %d %s", w.Code, w.Body)
	}
	var run session.Run
	if err := json.Unmarshal(w.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	defer func() { sup.Cancel(run.ID); sup.Wait() }()
	if w := request(first.update, "POST", "/update", ""); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "running chats") {
		t.Fatalf("update during turn: %d %s", w.Code, w.Body)
	}
}
