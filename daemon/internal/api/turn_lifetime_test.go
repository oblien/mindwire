package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

type detachedTurnAdapter struct {
	resolveFakeAdapter
	finish chan struct{}
	calls  atomic.Int32
}

func (f *detachedTurnAdapter) RunStream(ctx context.Context, _ agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	f.calls.Add(1)
	emit(agent.Event{Type: agent.EventSession, SessionID: "detached-native-session"})
	emit(agent.Event{Type: agent.EventText, Text: "Working while the app is closed"})
	select {
	case <-ctx.Done():
		return agent.TurnResult{}, ctx.Err()
	case <-f.finish:
		emit(agent.Event{Type: agent.EventResult, Result: &agent.ResultInfo{Text: "Completed in the background"}})
		return agent.TurnResult{Text: "Completed in the background"}, nil
	}
}

func TestClosingHTTPStreamLeavesNativeTurnRunningAndReplayable(t *testing.T) {
	fake := &detachedTurnAdapter{resolveFakeAdapter: resolveFakeAdapter{id: t.Name()}, finish: make(chan struct{})}
	agent.Register(fake)
	st, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	hub := stream.New()
	sup := orchestrator.New(st, hub, notify.Fanout(nil), t.TempDir(), fake.id)
	mux := http.NewServeMux()
	api := New(st, hub, sup)
	defer api.Close()
	api.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	client := server.Client()
	client.Timeout = 10 * time.Second
	response, err := client.Post(server.URL+"/turns", "application/json", strings.NewReader(`{"requestId":"detached","chatId":"chat","message":"Work"}`))
	if err != nil {
		t.Fatal(err)
	}
	var run session.Run
	err = json.NewDecoder(response.Body).Decode(&run)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusAccepted {
		t.Fatalf("turn: %v status=%d", err, response.StatusCode)
	}
	defer func() { sup.Cancel(run.ID); sup.Wait() }()
	streamURL := server.URL + "/runs/" + run.ID + "/stream"
	response, err = client.Get(streamURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	scanner := bufio.NewScanner(response.Body)
	var first string
	for scanner.Scan() {
		first += scanner.Text()
		if strings.Contains(first, "Working while the app is closed") {
			break
		}
	}
	if !strings.Contains(first, "Working while the app is closed") {
		t.Fatal("live transcript did not arrive")
	}
	response.Body.Close() // disconnect the entire viewer, not a Stop request
	current, _ := st.GetRun(run.ID)
	if current.Status != "running" || st.Session(fake.ID(), "chat") != "detached-native-session" {
		t.Fatalf("disconnect stopped the agent: %+v", current)
	}
	close(fake.finish)
	sup.Wait()
	response, err = client.Get(streamURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	current, _ = st.GetRun(run.ID)
	if err != nil || current.Status != "done" || fake.calls.Load() != 1 || !strings.Contains(string(data), "Completed in the background") {
		t.Fatalf("reopen lost or restarted the turn: status=%s calls=%d err=%v stream=%s", current.Status, fake.calls.Load(), err, data)
	}
}
