package mindwire

import (
	"errors"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// wantStatus asserts err is an *APIError with the given HTTP status.
func wantStatus(t *testing.T, op string, err error, status int) {
	t.Helper()
	var ae *APIError
	if !errors.As(err, &ae) {
		t.Fatalf("%s: want *APIError, got %T: %v", op, err, err)
	}
	if ae.Status != status {
		t.Fatalf("%s: status %d, want %d (%v)", op, ae.Status, status, err)
	}
}

// capsFor returns a registered adapter's declared capabilities (fatal if unregistered).
func capsFor(t *testing.T, id string) Capabilities {
	t.Helper()
	for _, a := range agent.All() {
		if a.ID() == id {
			return a.Capabilities()
		}
	}
	t.Fatalf("adapter %q not registered", id)
	return Capabilities{}
}

// The fake adapter implements neither optional module, so every Prompts call returns APIError{400} —
// the SDK's type-assert gate, identical to the HTTP handlers' capability check.
func TestPromptsUnsupportedAgent(t *testing.T) {
	c := newFakeClient(t, nil) // default agent = "fake"

	_, err := c.Prompts.Memory("")
	wantStatus(t, "Memory", err, 400)
	_, err = c.Prompts.SetMemory(MemoryProject, "", "x")
	wantStatus(t, "SetMemory", err, 400)
	_, err = c.Prompts.List("")
	wantStatus(t, "List", err, 400)
	_, err = c.Prompts.Get(MemoryUser, "", "x")
	wantStatus(t, "Get", err, 400)
	_, err = c.Prompts.Set(MemoryUser, "", "x", "y")
	wantStatus(t, "Set", err, 400)
	_, err = c.Prompts.Subagents("")
	wantStatus(t, "Subagents", err, 400)
	_, err = c.Prompts.Subagent(MemoryUser, "", "x")
	wantStatus(t, "Subagent", err, 400)
	_, err = c.Prompts.SetSubagent(MemoryUser, "", "x", "y")
	wantStatus(t, "SetSubagent", err, 400)
	_, err = c.Prompts.DeleteMemory(MemoryProject, "")
	wantStatus(t, "DeleteMemory", err, 400)
	err = c.Prompts.Delete(MemoryUser, "", "x")
	wantStatus(t, "Delete", err, 400)
	err = c.Prompts.DeleteSubagent(MemoryUser, "", "x")
	wantStatus(t, "DeleteSubagent", err, 400)
}

// TestPromptsDeleteRoundtrip exercises the three DELETE surfaces end-to-end through the real Claude
// adapter: each delete is idempotent when the target is absent, removes a written target (a follow-up
// read reflects the removal), and rejects a traversal name with 400 — mirroring DELETE /memory,
// /prompts/{name}, and /subagents/{name}.
func TestPromptsDeleteRoundtrip(t *testing.T) {
	home, proj := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	c := newFakeClient(t, nil).WithAgent("claude-code")

	// Memory: absent delete is a no-op that reports Exists=false; write then delete clears it.
	doc, err := c.Prompts.DeleteMemory(MemoryProject, proj)
	if err != nil || doc.Exists {
		t.Fatalf("DeleteMemory(absent) = %+v err=%v, want !Exists, nil", doc, err)
	}
	if _, err := c.Prompts.SetMemory(MemoryProject, proj, "# mem"); err != nil {
		t.Fatalf("SetMemory: %v", err)
	}
	if _, err := c.Prompts.DeleteMemory(MemoryProject, proj); err != nil {
		t.Fatalf("DeleteMemory: %v", err)
	}
	for _, d := range mustDocs(t, c, proj) {
		if d.Scope == MemoryProject && d.Exists {
			t.Fatalf("project memory still present after delete: %+v", d)
		}
	}

	// Prompt template: absent delete is idempotent; a written template is gone (Get → 404) after delete.
	if err := c.Prompts.Delete(MemoryProject, proj, "ghost"); err != nil {
		t.Fatalf("Delete(absent): %v", err)
	}
	if _, err := c.Prompts.Set(MemoryProject, proj, "greet", "Say hi"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := c.Prompts.Delete(MemoryProject, proj, "greet"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = c.Prompts.Get(MemoryProject, proj, "greet")
	wantStatus(t, "Get after Delete", err, 404)

	// Subagent definition: absent delete is idempotent; a written def is gone (Subagent → 404) after.
	if err := c.Prompts.DeleteSubagent(MemoryProject, proj, "ghost"); err != nil {
		t.Fatalf("DeleteSubagent(absent): %v", err)
	}
	if _, err := c.Prompts.SetSubagent(MemoryProject, proj, "reviewer", "---\nname: reviewer\n---\nBe thorough."); err != nil {
		t.Fatalf("SetSubagent: %v", err)
	}
	if err := c.Prompts.DeleteSubagent(MemoryProject, proj, "reviewer"); err != nil {
		t.Fatalf("DeleteSubagent: %v", err)
	}
	_, err = c.Prompts.Subagent(MemoryProject, proj, "reviewer")
	wantStatus(t, "Subagent after DeleteSubagent", err, 404)

	// Traversal names are rejected with 400 before any filesystem touch.
	err = c.Prompts.Delete(MemoryProject, proj, "../escape")
	wantStatus(t, "Delete(traversal)", err, 400)
	err = c.Prompts.DeleteSubagent(MemoryProject, proj, "../escape")
	wantStatus(t, "DeleteSubagent(traversal)", err, 400)
}

// mustDocs fetches the memory docs across scopes or fails the test.
func mustDocs(t *testing.T, c *Client, dir string) []MemoryDoc {
	t.Helper()
	docs, err := c.Prompts.Memory(dir)
	if err != nil {
		t.Fatalf("Memory: %v", err)
	}
	return docs
}

// A full subagent-definition roundtrip through the real Claude adapter: write (raw content canonical +
// parsed meta), get, list (content omitted), plus 404/400 gates. Codex has no module → every call 400s.
func TestSubagentsClaudeRoundtrip(t *testing.T) {
	home, proj := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	c := newFakeClient(t, nil).WithAgent("claude-code")

	body := "---\nname: reviewer\ndescription: Reviews code\ntools: Read, Grep\n---\nBe thorough."
	sub, err := c.Prompts.SetSubagent(MemoryProject, proj, "reviewer", body)
	if err != nil || sub.Content != body || sub.Meta == nil || sub.Meta.Description != "Reviews code" {
		t.Fatalf("SetSubagent = %+v err=%v", sub, err)
	}
	if len(sub.Meta.Tools) != 2 {
		t.Fatalf("parsed tools = %v, want 2", sub.Meta.Tools)
	}

	got, err := c.Prompts.Subagent(MemoryProject, proj, "reviewer")
	if err != nil || got.Content != body {
		t.Fatalf("Subagent = %+v err=%v", got, err)
	}
	list, err := c.Prompts.Subagents(proj)
	if err != nil || len(list) != 1 || list[0].Content != "" {
		t.Fatalf("Subagents = %+v err=%v, want 1 entry, content omitted", list, err)
	}

	_, err = c.Prompts.Subagent(MemoryProject, proj, "does-not-exist")
	wantStatus(t, "Subagent(missing)", err, 404)
	_, err = c.Prompts.Subagent(MemoryProject, proj, "../escape")
	wantStatus(t, "Subagent(traversal)", err, 400)

	// Codex declares no subagent-definition module.
	cx := newFakeClient(t, nil).WithAgent("codex")
	_, err = cx.Prompts.Subagents(proj)
	wantStatus(t, "Subagents(codex)", err, 400)
}

// A full memory + prompt-template roundtrip through the real Claude adapter (both scopes), against a
// temp CLAUDE_CONFIG_DIR and an explicit project dir.
func TestPromptsClaudeRoundtrip(t *testing.T) {
	home, proj := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	c := newFakeClient(t, nil).WithAgent("claude-code")

	// Memory absent at both scopes initially.
	docs, err := c.Prompts.Memory(proj)
	if err != nil {
		t.Fatalf("Memory: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("want 2 memory docs (project+user), got %d", len(docs))
	}
	for _, d := range docs {
		if d.Exists {
			t.Errorf("scope %s should be absent initially, got %+v", d.Scope, d)
		}
	}

	// Write project memory, read it back through the list.
	if _, err := c.Prompts.SetMemory(MemoryProject, proj, "# project memory"); err != nil {
		t.Fatalf("SetMemory(project): %v", err)
	}
	docs, _ = c.Prompts.Memory(proj)
	var project MemoryDoc
	for _, d := range docs {
		if d.Scope == MemoryProject {
			project = d
		}
	}
	if !project.Exists || project.Content != "# project memory" {
		t.Fatalf("project memory after write = %+v", project)
	}

	// Prompt template: write + get (content populated), list (content omitted).
	if _, err := c.Prompts.Set(MemoryProject, proj, "greet", "Say hi"); err != nil {
		t.Fatalf("Set(prompt): %v", err)
	}
	tpl, err := c.Prompts.Get(MemoryProject, proj, "greet")
	if err != nil || tpl.Content != "Say hi" {
		t.Fatalf("Get(prompt) = %+v err=%v", tpl, err)
	}
	list, err := c.Prompts.List(proj)
	if err != nil || len(list) != 1 || list[0].Content != "" {
		t.Fatalf("List = %+v err=%v, want 1 entry, content omitted", list, err)
	}

	// A missing template is 404; a traversal name is 400.
	_, err = c.Prompts.Get(MemoryProject, proj, "does-not-exist")
	wantStatus(t, "Get(missing)", err, 404)
	_, err = c.Prompts.Get(MemoryProject, proj, "../escape")
	wantStatus(t, "Get(traversal)", err, 400)
}

// Codex exposes memory at both scopes but saved prompts USER-only; a project-scope prompt op is 400.
// An empty scope defaults to user (mirroring the HTTP promptScope default).
func TestPromptsCodexUserOnly(t *testing.T) {
	home, proj := t.TempDir(), t.TempDir()
	t.Setenv("CODEX_HOME", home)
	c := newFakeClient(t, nil).WithAgent("codex")

	_, err := c.Prompts.Set(MemoryProject, proj, "x", "y")
	wantStatus(t, "Set(project)", err, 400)

	if _, err := c.Prompts.Set(MemoryUser, proj, "p", "body"); err != nil {
		t.Fatalf("Set(user): %v", err)
	}
	// Empty scope defaults to user, so this resolves the same template.
	if tpl, err := c.Prompts.Get("", proj, "p"); err != nil || tpl.Content != "body" {
		t.Fatalf("Get(default scope) = %+v err=%v", tpl, err)
	}
	if docs, err := c.Prompts.Memory(proj); err != nil || len(docs) != 2 {
		t.Fatalf("Memory = %d docs err=%v, want 2", len(docs), err)
	}
}

// The capability flags advertise the surface: on for the real adapters, off for the module-less fake.
func TestPromptsCapabilityFlags(t *testing.T) {
	for _, id := range []string{"claude-code", "codex", "opencode"} {
		if caps := capsFor(t, id); !caps.Memory || !caps.PromptTemplates {
			t.Errorf("%s: Memory=%v PromptTemplates=%v, want both true", id, caps.Memory, caps.PromptTemplates)
		}
	}
	if caps := capsFor(t, "fake"); caps.Memory || caps.PromptTemplates {
		t.Errorf("fake should not advertise memory/promptTemplates, got Memory=%v PromptTemplates=%v", caps.Memory, caps.PromptTemplates)
	}
	// Subagent definitions: on for claude-code and opencode (both have an on-disk agent-def store), off
	// for codex and the fake.
	for _, id := range []string{"claude-code", "opencode"} {
		if caps := capsFor(t, id); !caps.SubagentDefs {
			t.Errorf("%s: SubagentDefs=false, want true", id)
		}
	}
	for _, id := range []string{"codex", "fake"} {
		if caps := capsFor(t, id); caps.SubagentDefs {
			t.Errorf("%s should not advertise subagentDefs", id)
		}
	}
}
