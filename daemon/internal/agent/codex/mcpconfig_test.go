package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// W4b: a profile carries the per-run overlay via `-p`, which MUST precede the optional `resume`
// subcommand (codex rejects `-p` after `resume`).
func TestBuildExecCommandProfile(t *testing.T) {
	fresh := buildExecCommand(agent.TurnInput{Message: "hi"}, materialized{configProfile: "mindwire-abc"})
	if !strings.Contains(fresh, "codex exec -p 'mindwire-abc'") {
		t.Errorf("fresh command missing profile flag in position\n got: %s", fresh)
	}

	resume := buildExecCommand(agent.TurnInput{Message: "go", SessionID: "s1"}, materialized{configProfile: "mindwire-abc"})
	pIdx, rIdx := strings.Index(resume, "-p 'mindwire-abc'"), strings.Index(resume, "resume")
	if pIdx < 0 || rIdx < 0 || pIdx > rIdx {
		t.Errorf("resume command must place -p before `resume`\n got: %s", resume)
	}

	// No profile ⇒ no -p flag (the common path is unchanged).
	if plain := buildExecCommand(agent.TurnInput{Message: "hi"}, materialized{}); strings.Contains(plain, " -p ") {
		t.Errorf("no-profile command should not contain -p\n got: %s", plain)
	}
}

// decodeMCPServers accepts both the wrapped `{"mcpServers":{…}}` and the bare `{NAME:{…}}` shapes, and
// treats empty input as no servers (not an error).
func TestDecodeMCPServers(t *testing.T) {
	wrapped, err := decodeMCPServers(json.RawMessage(`{"mcpServers":{"a":{"command":"x"}}}`))
	if err != nil || len(wrapped) != 1 || wrapped["a"].Command != "x" {
		t.Fatalf("wrapped decode = %+v err=%v", wrapped, err)
	}
	bare, err := decodeMCPServers(json.RawMessage(`{"b":{"command":"y","args":["1"]}}`))
	if err != nil || len(bare) != 1 || bare["b"].Command != "y" || len(bare["b"].Args) != 1 {
		t.Fatalf("bare decode = %+v err=%v", bare, err)
	}
	if got, err := decodeMCPServers(nil); err != nil || got != nil {
		t.Fatalf("empty decode = %+v err=%v, want nil,nil", got, err)
	}
}

// buildConfigOverlay renders valid TOML: the top-level model_instructions_file MUST precede any table
// header, mcp servers become `[mcp_servers.NAME]` with an `.env` sub-table, and strings are escaped.
func TestBuildConfigOverlay(t *testing.T) {
	servers := map[string]codexMCPServer{
		"demo": {Command: "echo", Args: []string{"hi", "there"}, Env: map[string]string{"TOKEN": "s\"h"}, Cwd: "/w"},
	}
	out := buildConfigOverlay("/tmp/sys.md", servers)

	miIdx, tblIdx := strings.Index(out, "model_instructions_file ="), strings.Index(out, "[mcp_servers.demo]")
	if miIdx < 0 || tblIdx < 0 || miIdx > tblIdx {
		t.Fatalf("model_instructions_file must precede the first table\n%s", out)
	}
	for _, want := range []string{
		`model_instructions_file = "/tmp/sys.md"`,
		"[mcp_servers.demo]",
		`command = "echo"`,
		`args = ["hi", "there"]`,
		`cwd = "/w"`,
		"[mcp_servers.demo.env]",
		`TOKEN = "s\"h"`, // the inner quote is escaped
	} {
		if !strings.Contains(out, want) {
			t.Errorf("overlay missing %q\n%s", want, out)
		}
	}
}

// materialize writes the overlay into CODEX_HOME (keeping prompt text and MCP env OFF the command line)
// and cleanup removes both the overlay and the referenced system-prompt file.
func TestMaterializeOverlay(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)

	const secret = "super-secret-token"
	opts := agent.TurnOptions{
		SystemPrompt: "You are terse.",
		MCPServers:   json.RawMessage(`{"demo":{"command":"echo","env":{"TOKEN":"` + secret + `"}}}`),
	}
	files, _, cleanup, err := materialize(agent.TurnInput{Options: opts})
	defer cleanup()
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if files.configProfile == "" {
		t.Fatal("expected a config profile")
	}

	overlay := filepath.Join(home, files.configProfile+".config.toml")
	body, err := os.ReadFile(overlay)
	if err != nil {
		t.Fatalf("read overlay: %v", err)
	}
	// The MCP env secret lives in the overlay file, NOT on argv.
	if !strings.Contains(string(body), secret) {
		t.Errorf("overlay should carry the MCP env value\n%s", body)
	}
	cmd := buildExecCommand(agent.TurnInput{Message: "hi", Options: opts}, files)
	if strings.Contains(cmd, secret) || strings.Contains(cmd, "You are terse.") {
		t.Errorf("prompt text / MCP env must stay off the command line\n got: %s", cmd)
	}

	// The prompt itself was written to a separate file referenced by model_instructions_file.
	var promptPath string
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "model_instructions_file = ") {
			promptPath = strings.Trim(strings.TrimPrefix(line, "model_instructions_file = "), `"`)
		}
	}
	if promptPath == "" {
		t.Fatal("overlay missing model_instructions_file")
	}
	if pb, err := os.ReadFile(promptPath); err != nil || string(pb) != "You are terse." {
		t.Fatalf("prompt file = %q err=%v", pb, err)
	}

	cleanup()
	if _, err := os.Stat(overlay); !os.IsNotExist(err) {
		t.Errorf("cleanup should remove the overlay (stat err=%v)", err)
	}
	if _, err := os.Stat(promptPath); !os.IsNotExist(err) {
		t.Errorf("cleanup should remove the prompt file (stat err=%v)", err)
	}
}

// The interactive-approval (app-server) transport can't take the `-p` overlay, so a turn that pairs a
// non-`never` approval policy + inbound channel with systemPrompt/mcpServers is rejected explicitly
// (an error result), never silently dropped.
func TestRunStreamAppServerRejectsOverlayOptions(t *testing.T) {
	inbound := make(chan agent.Inbound)
	in := agent.TurnInput{
		Message: "hi",
		Config:  map[string]string{keyApproval: "on-request"},
		Options: agent.TurnOptions{SystemPrompt: "You are terse."},
		Inbound: inbound,
	}
	var errMsg string
	res, err := adapter{}.RunStream(context.Background(), in, func(e agent.Event) {
		if e.Type == agent.EventError {
			errMsg = e.Error
		}
	})
	if err != nil {
		t.Fatalf("RunStream returned a transport error: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Text, "exec transport") {
		t.Fatalf("expected an explicit exec-transport error, got %+v", res)
	}
	if !strings.Contains(errMsg, "exec transport") {
		t.Errorf("expected an error event explaining the limitation, got %q", errMsg)
	}
}
