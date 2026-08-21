package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

// newChatTestEnv wires the real supervisor (claude-code default) but hands back the store too, so a
// test can seed sessions/messages directly and assert the bookkeeping side effects of the lifecycle
// routes. cwd is the daemon working directory (also where the fake native transcript is path-scoped).
func newChatTestEnv(t *testing.T, cwd string) (http.Handler, *session.Store) {
	t.Helper()
	store, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	sup := orchestrator.New(store, stream.New(), notify.Fanout(nil), cwd, "claude-code")
	mux := http.NewServeMux()
	New(store, stream.New(), sup).Register(mux)
	return mux, store
}

// claudeTranscriptPath mirrors the claude adapter's project-scoped transcript path:
// <CLAUDE_CONFIG_DIR>/projects/<slug(cwd)>/<sid>.jsonl, slug = the abs cwd with separators → '-'.
func claudeTranscriptPath(base, cwd, sid string) string {
	slug := strings.NewReplacer("/", "-", "\\", "-").Replace(cwd)
	return filepath.Join(base, "projects", slug, sid+".jsonl")
}

// TestRenameChatHTTP: PUT /chats/{id} sets a user title that wins over the derived first-message
// snippet in every listing; an empty title clears it.
func TestRenameChatHTTP(t *testing.T) {
	h, store := newChatTestEnv(t, t.TempDir())
	if err := store.AddMessage(session.Message{ID: "m1", ChatID: "c1", Role: "user", Text: "original snippet", CreatedAt: "t1"}); err != nil {
		t.Fatalf("add message: %v", err)
	}

	rec := serve(t, h, "PUT", "/chats/c1", `{"title":"My Title"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /chats/c1: %d %s", rec.Code, rec.Body.String())
	}
	var sum session.ChatSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &sum); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if sum.Title != "My Title" {
		t.Fatalf("rename summary title = %q, want %q", sum.Title, "My Title")
	}

	// The listing reflects the user title (it wins over the derived snippet).
	rec = serve(t, h, "GET", "/chats", "")
	var chats []session.ChatSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &chats); err != nil {
		t.Fatalf("decode chats: %v", err)
	}
	if len(chats) != 1 || chats[0].Title != "My Title" {
		t.Fatalf("chats = %+v, want user title to win", chats)
	}

	// Empty title clears the rename → back to the derived snippet.
	if rec := serve(t, h, "PUT", "/chats/c1", `{"title":""}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT clear: %d %s", rec.Code, rec.Body.String())
	}
	rec = serve(t, h, "GET", "/chats", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &chats)
	if chats[0].Title != "original snippet" {
		t.Fatalf("after clear title = %q, want derived snippet", chats[0].Title)
	}
}

// TestDeleteChatHTTP: DELETE /chats/{id} purges all bookkeeping AND removes the agent's native
// transcript (source of truth), reporting which agents' transcripts were purged.
func TestDeleteChatHTTP(t *testing.T) {
	home, proj := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	h, store := newChatTestEnv(t, proj)

	// Seed a claude session for the chat and write a fake native transcript at the path the adapter
	// will target for deletion.
	if err := store.SetSession("claude-code", "c1", "sid1"); err != nil {
		t.Fatalf("set session: %v", err)
	}
	if err := store.SetChatCWD("c1", proj); err != nil {
		t.Fatalf("set cwd: %v", err)
	}
	if err := store.AddMessage(session.Message{ID: "m1", ChatID: "c1", Role: "user", Text: "hi", CreatedAt: "t1"}); err != nil {
		t.Fatalf("add message: %v", err)
	}
	transcript := claudeTranscriptPath(home, proj, "sid1")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(transcript, []byte(`{"type":"user"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	rec := serve(t, h, "DELETE", "/chats/c1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /chats/c1: %d %s", rec.Code, rec.Body.String())
	}
	var res deleteResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if !res.Deleted || res.Sessions != 1 {
		t.Fatalf("delete result = %+v, want deleted=true sessions=1", res)
	}
	if len(res.NativePurged) != 1 || res.NativePurged[0] != "claude-code" {
		t.Fatalf("nativePurged = %v, want [claude-code]", res.NativePurged)
	}
	if len(res.NativeFailed) != 0 {
		t.Fatalf("nativeFailed = %v, want none", res.NativeFailed)
	}

	// The native transcript is gone.
	if _, err := os.Stat(transcript); !os.IsNotExist(err) {
		t.Fatalf("native transcript still present (stat err=%v)", err)
	}
	// Bookkeeping is purged.
	if store.Session("claude-code", "c1") != "" {
		t.Fatalf("session mapping survived delete")
	}
	if len(store.Messages("c1")) != 0 {
		t.Fatalf("messages survived delete")
	}
}

// TestCompactChatHTTP exercises the compact-now gates (POST /chats/{id}/compact). The happy path spawns
// a real run (and thus the CLI), so this asserts only the deterministic gate wiring that returns before
// StartCompact: unknown agent, the no-session guard, and — with a session seeded so the no-session gate
// passes — a malformed body. The claude-code default implements CompactModule, so the module gate can't
// be hit here; the SDK/openapi parity tests cover the route's existence.
func TestCompactChatHTTP(t *testing.T) {
	h, store := newChatTestEnv(t, t.TempDir())

	// No session yet → 400 "no conversation to compact yet".
	rec := serve(t, h, "POST", "/chats/c1/compact", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("compact without session: %d %s, want 400", rec.Code, rec.Body.String())
	}
	var errBody struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
	if !strings.Contains(errBody.Error, "no conversation to compact") {
		t.Fatalf("no-session error = %q, want the compact-gate message", errBody.Error)
	}

	// Unknown agent → 400 "unknown agent" (before any chat/session lookup).
	if rec := serve(t, h, "POST", "/chats/c1/compact?agent=ghost", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("compact unknown agent: %d, want 400", rec.Code)
	}

	// With a session, the no-session gate passes and a malformed body is a 400 (reached only after the
	// gate, so this proves both the gate order and that a valid session clears it — without spawning a run).
	if err := store.SetSession("claude-code", "c1", "sid1"); err != nil {
		t.Fatalf("set session: %v", err)
	}
	rec = serve(t, h, "POST", "/chats/c1/compact", `{"instructions":`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("compact malformed body: %d %s, want 400", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
	if !strings.Contains(errBody.Error, "invalid body") {
		t.Fatalf("malformed-body error = %q, want %q", errBody.Error, "invalid body")
	}
}

// TestForkChatHTTP: POST /chats/{id}/fork seeds the new chat's session mapping from the source; an
// unknown source is 404 and a target id already in use is 400.
func TestForkChatHTTP(t *testing.T) {
	h, store := newChatTestEnv(t, t.TempDir())
	if err := store.SetSession("claude-code", "src", "sid-src"); err != nil {
		t.Fatalf("set session: %v", err)
	}
	if err := store.AddMessage(session.Message{ID: "m1", ChatID: "src", Role: "user", Text: "hi", CreatedAt: "t1"}); err != nil {
		t.Fatalf("add message: %v", err)
	}

	rec := serve(t, h, "POST", "/chats/src/fork", `{"newChatId":"dst"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST fork: %d %s", rec.Code, rec.Body.String())
	}
	var sum session.ChatSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &sum); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if sum.ChatID != "dst" {
		t.Fatalf("fork summary chatId = %q, want dst", sum.ChatID)
	}
	// The fork shares the source's native session until its first turn branches it.
	if store.Session("claude-code", "dst") != "sid-src" {
		t.Fatalf("fork did not seed the session mapping: %q", store.Session("claude-code", "dst"))
	}

	// Unknown source → 404.
	if rec := serve(t, h, "POST", "/chats/ghost/fork", `{"newChatId":"x"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("fork unknown source: %d, want 404", rec.Code)
	}
	// Target id already in use → 400.
	if rec := serve(t, h, "POST", "/chats/src/fork", `{"newChatId":"dst"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("fork onto existing id: %d, want 400", rec.Code)
	}

	// An empty body is allowed: the id is generated and the fork still succeeds.
	rec = serve(t, h, "POST", "/chats/src/fork", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("fork with generated id: %d %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &sum)
	if sum.ChatID == "" || sum.ChatID == "src" {
		t.Fatalf("generated fork id = %q, want a fresh non-empty id", sum.ChatID)
	}
}
