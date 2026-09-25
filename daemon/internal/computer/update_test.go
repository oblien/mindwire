package computer

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestManualPromotionKeepsTheRequestAndSurvivesOldControllerStatusWrites(t *testing.T) {
	t.Setenv("MINDWIRE_COMPUTER_MANAGED", "1")
	s, dir := fixture(t)
	mux := http.NewServeMux()
	s.Register(mux)
	request := func(body string) Update {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/computer/update", bytes.NewBufferString(body)))
		var update Update
		if w.Code != 202 || json.Unmarshal(w.Body.Bytes(), &update) != nil {
			t.Fatalf("update: %d %s", w.Code, w.Body.String())
		}
		return update
	}
	queued := request(`{"version":"0.1.27"}`)
	if queued.Force || s.ForceUpdatePending() {
		t.Fatal("automatic request forced a restart")
	}
	manual := request(`{"version":"0.1.27","force":true}`)
	if manual.ID != queued.ID || !manual.Force || !s.ForceUpdatePending() {
		t.Fatalf("promotion lost the request: %+v", manual)
	}
	// Old controllers rewrite the status without knowing about the force field.
	queued.Status = "waiting"
	data, _ := json.Marshal(queued)
	if err := os.WriteFile(s.updatePath(), data, 0600); err != nil {
		t.Fatal(err)
	}
	if !s.ForceUpdatePending() {
		t.Fatal("controller overwrote manual intent")
	}
	if again := request(`{"version":"0.1.27"}`); again.ID != manual.ID || !again.Force {
		t.Fatal("an automatic retry downgraded owner intent")
	}
	reopened, err := New(dir, s.apiAddress, s.apiToken, s.registryID)
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.ForceUpdatePending() {
		t.Fatal("manual intent was lost after reload")
	}
	queued.Status = "complete"
	data, _ = json.Marshal(queued)
	if err := os.WriteFile(s.updatePath(), data, 0600); err != nil {
		t.Fatal(err)
	}
	if s.ForceUpdatePending() {
		t.Fatal("a completed request authorized an unrelated restart")
	}
	next := request(`{"version":"0.1.28"}`)
	if next.ID == manual.ID || next.Force || s.ForceUpdatePending() {
		t.Fatal("new automatic request inherited force")
	}
}
