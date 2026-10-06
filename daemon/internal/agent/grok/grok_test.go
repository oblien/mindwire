package grok

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestRunACPStreamsNativeUpdates(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "grok")
	// A protocol-faithful fake: each request gets its matching response, while
	// prompt progress travels as ACP notifications before prompt settlement.
	script := `#!/bin/sh
read line; echo '{"jsonrpc":"2.0","id":1,"result":{"authMethods":[]}}'
read line; echo '{"jsonrpc":"2.0","id":2,"result":{"sessionId":"native-1"}}'
read line
printf '%s' "$line" > "$MINDWIRE_GROK_PROMPT_CHECK"
echo '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_thought_chunk","content":{"text":"plan "}}}}'
echo '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call","toolCallId":"tool-1","title":"Read","rawInput":{"path":"a.go"}}}}'
echo '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call_update","toolCallId":"tool-1","status":"completed","content":{"text":"package a"}}}}'
echo '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"text":"done"}}}}'
echo '{"jsonrpc":"2.0","id":3,"result":{"stopReason":"end_turn"}}'
`
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	promptPath := filepath.Join(dir, "prompt.json")
	t.Setenv("MINDWIRE_GROK_PROMPT_CHECK", promptPath)
	var events []agent.Event
	attachment := agent.Attachment{ArtifactID: "uploaded-file", Name: "notes.txt", Mime: "text/plain", Path: filepath.Join(dir, "notes.txt")}
	got, err := runACP(context.Background(), agent.TurnInput{Message: "hi", CWD: dir,
		Options: agent.TurnOptions{Attachments: []agent.Attachment{attachment}}}, func(e agent.Event) { events = append(events, e) })
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "done" || got.SessionID != "native-1" {
		t.Fatalf("result = %#v", got)
	}
	var text, thinking, use, result bool
	for _, e := range events {
		text = text || e.Type == agent.EventText
		thinking = thinking || e.Type == agent.EventThinking
		use = use || e.Type == agent.EventToolUse
		result = result || e.Type == agent.EventResult
	}
	if !text || !thinking || !use || !result {
		t.Fatalf("ACP updates not normalized: %#v", events)
	}
	data, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatal(err)
	}
	var prompt struct {
		Params struct {
			Prompt []struct {
				Text string `json:"text"`
			} `json:"prompt"`
		} `json:"params"`
	}
	if json.Unmarshal(data, &prompt) != nil || len(prompt.Params.Prompt) != 1 || !strings.Contains(prompt.Params.Prompt[0].Text, attachment.Path) {
		t.Fatal("uploaded file path missing from native ACP prompt")
	}
}

func TestACPCWDIsAlwaysAbsolute(t *testing.T) {
	cwd, err := acpCWD(agent.TurnInput{})
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(cwd) {
		t.Fatalf("ACP cwd = %q, want absolute path", cwd)
	}

	configured, err := acpCWD(agent.TurnInput{CWD: "relative/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(configured) {
		t.Fatalf("configured ACP cwd = %q, want absolute path", configured)
	}
}

func TestACPSessionContextIsNeverSilentlyDroppedOnResume(t *testing.T) {
	_, err := runACP(context.Background(), agent.TurnInput{
		SessionID: "existing",
		Options: agent.TurnOptions{
			SystemPrompt: "new instruction",
		},
	}, func(agent.Event) {})
	if err == nil || !strings.Contains(err.Error(), "only when creating a new session") {
		t.Fatalf("resume context error = %v", err)
	}
}

func TestGrokMCPCommandShapes(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "grok")
	argsPath := filepath.Join(dir, "args")
	script := `#!/bin/sh
printf '%s' "$*" > "$GROK_ARGS"
case "$*" in
  *'mcp list '*) printf '%s\n' '{"servers":[{"name":"filesystem","command":"npx","args":["-y","server"]}]}' ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GROK_ARGS", argsPath)

	servers, err := (adapter{}).ListMCPServers(agent.MemoryUser, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := servers["filesystem"].Command; got != "npx" {
		t.Fatalf("listed command = %q", got)
	}
	if err := (adapter{}).SetMCPServer(agent.MemoryUser, dir, "filesystem", agent.MCPServer{Command: "npx", Args: []string{"-y", "server"}}); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(args), "mcp add filesystem -- npx -y server"; got != want {
		t.Fatalf("user MCP args = %q, want %q", got, want)
	}
	if _, err := (adapter{}).ListMCPServers(agent.MemoryProject, dir); err == nil {
		t.Fatal("project scope should be rejected instead of using unverified CLI semantics")
	}
}

func TestRunACPBridgesPermissionResponse(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "grok")
	script := `#!/bin/sh
read line; echo '{"jsonrpc":"2.0","id":1,"result":{"authMethods":[]}}'
read line; echo '{"jsonrpc":"2.0","id":2,"result":{"sessionId":"native-ask"}}'
read line
echo '{"jsonrpc":"2.0","id":99,"method":"session/request_permission","params":{"toolCall":{"title":"Write main.go"},"options":[{"optionId":"allow-once","name":"Allow once"},{"optionId":"reject","name":"Reject"}]}}'
read decision
case "$decision" in *allow-once*) ;; *) exit 7 ;; esac
echo '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"text":"approved"}}}}'
echo '{"jsonrpc":"2.0","id":3,"result":{"stopReason":"end_turn"}}'
`
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	inbound := make(chan agent.Inbound, 1)
	got, err := runACP(context.Background(), agent.TurnInput{Message: "hi", CWD: dir, Config: map[string]string{keyPermission: "ask"}, Inbound: inbound}, func(e agent.Event) {
		if e.Type == agent.EventInteraction {
			inbound <- agent.Inbound{Kind: "response", InteractionID: e.Interaction.ID, Decision: "allow-once"}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "approved" || got.SessionID != "native-ask" {
		t.Fatalf("result = %#v", got)
	}
}
