package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// TestCodexMemoryModule verifies the adapter wires the shared MemoryLayout to Codex's paths: AGENTS.md
// at both scopes, but saved prompts USER-only (no project-scope convention). Edge cases live in
// agent/memfile_test.go; this asserts the Codex-specific wiring against a temp CODEX_HOME.
func TestCodexMemoryModule(t *testing.T) {
	home, proj := t.TempDir(), t.TempDir()
	t.Setenv("CODEX_HOME", home)
	a := adapter{}

	if caps := a.Capabilities(); !caps.Memory || !caps.PromptTemplates {
		t.Fatalf("codex caps: Memory=%v PromptTemplates=%v, want both true", caps.Memory, caps.PromptTemplates)
	}

	// Memory: both scopes, AGENTS.md at project <dir>/ and user <home>/.
	if scopes := a.MemoryScopes(); len(scopes) != 2 {
		t.Fatalf("MemoryScopes = %v, want project+user", scopes)
	}
	if _, err := a.WriteMemory(agent.MemoryProject, proj, "# agents"); err != nil {
		t.Fatalf("WriteMemory(project): %v", err)
	}
	if doc, err := a.ReadMemory(agent.MemoryProject, proj); err != nil ||
		doc.Content != "# agents" || doc.Path != filepath.Join(proj, "AGENTS.md") {
		t.Fatalf("project memory = %+v err=%v", doc, err)
	}

	// Prompts: user-only. A project-scope op is an unsupported-scope error (→ 400 at the API).
	if scopes := a.PromptScopes(); len(scopes) != 1 || scopes[0] != agent.MemoryUser {
		t.Fatalf("PromptScopes = %v, want [user]", scopes)
	}
	if _, err := a.WritePrompt(agent.MemoryProject, proj, "x", "y"); err == nil {
		t.Error("codex project-scope prompt write should be unsupported")
	}
	if _, err := a.WritePrompt(agent.MemoryUser, proj, "p", "body"); err != nil {
		t.Fatalf("WritePrompt(user): %v", err)
	}
	if got, err := a.ReadPrompt(agent.MemoryUser, proj, "p"); err != nil ||
		got.Content != "body" || got.Path != filepath.Join(home, "prompts", "p.md") {
		t.Fatalf("user prompt = %+v err=%v", got, err)
	}
}

// stickyPromptFile extracts the model_instructions_file target from a profile overlay and returns its
// contents — the effective system prompt Codex will load for the turn.
func stickyPromptFile(t *testing.T, home, profile string) string {
	t.Helper()
	overlay := filepath.Join(home, profile+".config.toml")
	body, err := os.ReadFile(overlay)
	if err != nil {
		t.Fatalf("read overlay %s: %v", overlay, err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "model_instructions_file = ") {
			p := strings.Trim(strings.TrimPrefix(line, "model_instructions_file = "), `"`)
			data, rerr := os.ReadFile(p)
			if rerr != nil {
				t.Fatalf("read prompt file %s: %v", p, rerr)
			}
			return string(data)
		}
	}
	t.Fatalf("overlay missing model_instructions_file:\n%s", body)
	return ""
}

// The sticky `system-prompt` config key backs the per-turn override: a turn with no Options.SystemPrompt
// but a sticky Config value still produces the overlay; a per-turn Options.SystemPrompt wins over it.
func TestMaterializeStickySystemPrompt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)

	// Sticky config prompt alone (no per-turn override) produces the overlay.
	sticky := agent.TurnInput{Config: map[string]string{keySystemPrompt: "You are sticky."}}
	files, _, cleanup, err := materialize(sticky)
	if err != nil {
		cleanup()
		t.Fatalf("materialize(sticky): %v", err)
	}
	if files.configProfile == "" {
		cleanup()
		t.Fatal("sticky system-prompt should produce a config profile")
	}
	if got := stickyPromptFile(t, home, files.configProfile); got != "You are sticky." {
		t.Errorf("sticky prompt = %q, want %q", got, "You are sticky.")
	}
	cleanup()

	// Per-turn Options.SystemPrompt WINS over the sticky Config value.
	override := agent.TurnInput{
		Config:  map[string]string{keySystemPrompt: "sticky"},
		Options: agent.TurnOptions{SystemPrompt: "per-turn wins"},
	}
	files2, _, cleanup2, err := materialize(override)
	defer cleanup2()
	if err != nil {
		t.Fatalf("materialize(override): %v", err)
	}
	if got := stickyPromptFile(t, home, files2.configProfile); got != "per-turn wins" {
		t.Errorf("effective prompt = %q, want per-turn override to win", got)
	}
}
