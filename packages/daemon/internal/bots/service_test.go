package bots

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/oblien/mindwire/packages/daemon/internal/agent"
	_ "github.com/oblien/mindwire/packages/daemon/internal/agent/claude"
	_ "github.com/oblien/mindwire/packages/daemon/internal/agent/codex"
	_ "github.com/oblien/mindwire/packages/daemon/internal/agent/opencode"
	"github.com/oblien/mindwire/packages/daemon/internal/registry"
	"github.com/oblien/mindwire/packages/daemon/internal/runner"
	"github.com/oblien/mindwire/packages/daemon/internal/session"
	"github.com/oblien/mindwire/packages/daemon/internal/surface"
)

type tokenTransport struct{ token string }

func (t tokenTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(copy)
}

func testService(t *testing.T) (*Service, registry.Bot) {
	t.Helper()
	dir := t.TempDir()
	db, err := registry.Open(filepath.Join(dir, "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = db.Import(registry.Import{Agents: []registry.Agent{{Record: registry.Record{ID: "agent"}, Name: "AI", AgentType: "codex"}}, Projects: []registry.Project{{Record: registry.Record{ID: "project"}, Name: "General", Path: dir}}, Chats: []registry.Chat{{Record: registry.Record{ID: "chat"}, AgentID: "agent", ProjectID: "project", Title: "Main chat"}}}); err != nil {
		t.Fatal(err)
	}
	master, err := db.EnsureMaster("agent", "project")
	if err != nil {
		t.Fatal(err)
	}
	history, err := session.Open(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return New(db, history, &sync.Mutex{}), master
}

func connect(t *testing.T, s *Service, chatID, harness string, ctx context.Context) (*mcp.ClientSession, runner.Turn, func()) {
	t.Helper()
	var adapter agent.Adapter
	for _, a := range agent.All() {
		if a.ID() == harness {
			adapter = a
		}
	}
	turn, cleanup, err := s.Bind(ctx, adapter, runner.Turn{ChatID: chatID, Options: agent.TurnOptions{MCPServers: json.RawMessage(`{"existing":{"url":"http://example.invalid/mcp"}}`)}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(turn.Options.MCPServers), turn.Environment[tokenEnv]) {
		t.Fatal("private token leaked into overlay")
	}
	var config struct {
		Servers map[string]struct {
			URL string `json:"url"`
		} `json:"mcpServers"`
	}
	if err = json.Unmarshal(turn.Options.MCPServers, &config); err != nil {
		t.Fatal(err)
	}
	if config.Servers["existing"].URL == "" {
		t.Fatal("lost existing tool server")
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "bot-test", Version: "1"}, nil)
	connection, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: config.Servers[mcpName].URL, HTTPClient: &http.Client{Transport: tokenTransport{turn.Environment[tokenEnv]}, Timeout: 5 * time.Second}}, nil)
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	return connection, turn, func() { connection.Close(); cleanup() }
}

func call(t *testing.T, c *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := c.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil || result.IsError {
		t.Fatalf("%s: %+v %v", name, result, err)
	}
	return result
}

func TestMasterCreatesInspectsAndImprovesBotsThroughRealMCP(t *testing.T) {
	s, master := testService(t)
	for _, harness := range []string{"codex", "claude-code"} {
		t.Run(harness, func(t *testing.T) {
			client, turn, cleanup := connect(t, s, "chat", harness, t.Context())
			defer cleanup()
			if !strings.Contains(turn.Options.ContextInstructions, "You are the manager bot") || !strings.Contains(turn.Options.ContextInstructions, "bots_update") {
				t.Fatal("master does not know its management role")
			}
			call(t, client, "bots_create", map[string]any{"name": "Writing Partner " + harness, "purpose": "Write clearly", "instructions": "Use clear prose"})
			bots, err := s.db.Bots(master.ProjectID, false)
			if err != nil {
				t.Fatal(err)
			}
			var bot registry.Bot
			for _, b := range bots {
				if b.Name == "Writing Partner "+harness {
					bot = b
				}
			}
			if bot.ID == "" {
				t.Fatal("creation tool did not save a bot")
			}
			call(t, client, "bots_list", map[string]any{})
			call(t, client, "bots_get", map[string]any{"botId": bot.ID})
			call(t, client, "knowledge_put", map[string]any{"botId": bot.ID, "title": "Style guide", "content": "Always use active voice. Keep paragraphs short.", "expectedRevision": bot.Revision})
			bot, _ = s.db.Bot(bot.ID)
			call(t, client, "bots_update", map[string]any{"botId": bot.ID, "icon": "blue-spark", "instructions": "Ask who the audience is, then write clear prose.", "memory": "User prefers short answers.", "expectedRevision": bot.Revision})
			bot, _ = s.db.Bot(bot.ID)
			if len(bot.Knowledge) != 1 || bot.Memory != "User prefers short answers." || !strings.Contains(bot.Instructions, "audience") || bot.Icon != "blue-spark" {
				t.Fatalf("improvement lost context: %+v", bot)
			}
			result := call(t, client, "bots_create_chat", map[string]any{"botId": bot.ID, "title": "My article", "requestId": "article"})
			if !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "chat_created") {
				t.Fatal("missing navigation result")
			}
			call(t, client, "bots_create_chat", map[string]any{"botId": bot.ID, "title": "My article", "requestId": "article"})
			chats, _ := s.db.BotChats(bot.ID)
			if len(chats) != 1 {
				t.Fatal("retry duplicated bot chat")
			}
			call(t, client, "bots_chats", map[string]any{"botId": bot.ID})
			call(t, client, "bots_read_chat", map[string]any{"chatId": chats[0].ID})
		})
	}
}

func TestChildToolsUseOwnKnowledgeMemoryAndRejectManagement(t *testing.T) {
	s, master := testService(t)
	bot, err := s.db.CreateBot(registry.Bot{AgentID: master.AgentID, ProjectID: master.ProjectID, Name: "Study Coach", Purpose: "Teach", Instructions: "Ask one question at a time", Knowledge: []registry.BotKnowledge{{ID: "guide", Title: "Course notes", Content: "Photosynthesis turns light into chemical energy."}}})
	if err != nil {
		t.Fatal(err)
	}
	chat, err := s.db.CreateBotChat(bot.ID, "study-chat", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	client, turn, cleanup := connect(t, s, chat.ID, "codex", ctx)
	defer cleanup()
	if strings.Contains(turn.Options.ContextInstructions, "You are the manager bot") || !strings.Contains(turn.Options.ContextInstructions, "Study Coach") {
		t.Fatal("child received wrong persona")
	}
	tools, err := client.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if strings.HasPrefix(tool.Name, "bots_") {
			t.Fatal("child received management tool")
		}
	}
	result := call(t, client, "knowledge_search", map[string]any{"query": "photosynthesis"})
	if !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "chemical energy") {
		t.Fatal("knowledge unavailable")
	}
	call(t, client, "knowledge_read", map[string]any{"sourceId": "guide"})
	for _, name := range []string{"knowledge_search", "memory_read", "memory_update"} {
		args := map[string]any{"botId": master.ID}
		if name == "knowledge_search" {
			args["query"] = "anything"
		}
		if name == "memory_update" {
			args["memory"] = "changed"
			args["expectedRevision"] = master.Revision
		}
		result, err = client.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || !result.IsError {
			t.Fatalf("child crossed bot scope: %s %+v %v", name, result, err)
		}
	}
	call(t, client, "memory_update", map[string]any{"memory": "User is studying biology.", "expectedRevision": bot.Revision})
	saved, _ := s.db.Bot(bot.ID)
	if saved.Memory != "User is studying biology." || len(saved.Knowledge) != 1 {
		t.Fatal("memory update lost knowledge")
	}
	cancel()
	time.Sleep(20 * time.Millisecond)
	if _, err = client.ListTools(t.Context(), nil); err == nil {
		t.Fatal("ended run retained tool capability")
	}
}

func TestDeveloperTurnDoesNotReceiveBotPromptOrTools(t *testing.T) {
	s, _ := testService(t)
	var adapter agent.Adapter
	for _, a := range agent.All() {
		if a.ID() == "codex" {
			adapter = a
		}
	}
	turn, cleanup, err := s.Bind(t.Context(), adapter, runner.Turn{ChatID: "developer-chat", Message: "Write code"})
	defer cleanup()
	if err != nil || turn.Options.SystemPrompt != "" || turn.Options.ContextInstructions != "" || len(turn.Options.MCPServers) != 0 || len(turn.Environment) != 0 {
		t.Fatalf("Developer changed: %+v %v", turn, err)
	}
}

func TestBotToolsComposeWithDesktopAndExistingMCPServers(t *testing.T) {
	s, _ := testService(t)
	desktop, err := surface.New(s.db)
	if err != nil {
		t.Fatal(err)
	}
	defer desktop.Close()
	for _, adapter := range agent.All() {
		if adapter.ID() != "codex" && adapter.ID() != "claude-code" {
			continue
		}
		t.Run(adapter.ID(), func(t *testing.T) {
			overlay, env, finishDesktop, err := desktop.BindRun(t.Context(), surface.Actor{Kind: "agent", RunID: adapter.ID(), ChatID: "chat"}, adapter.ID(), json.RawMessage(`{"existing":{"command":"example-mcp"}}`))
			if err != nil {
				t.Fatal(err)
			}
			defer finishDesktop()
			turn, finishBot, err := s.Bind(t.Context(), adapter, runner.Turn{ChatID: "chat", Environment: env, Options: agent.TurnOptions{MCPServers: overlay}})
			if err != nil {
				t.Fatal(err)
			}
			defer finishBot()
			var config struct {
				Servers map[string]agent.MCPServer `json:"mcpServers"`
			}
			if err := json.Unmarshal(turn.Options.MCPServers, &config); err != nil {
				t.Fatal(err)
			}
			if len(config.Servers) != 3 {
				t.Fatalf("want existing, desktop and bot servers, got %d entries", len(config.Servers))
			}
			for _, name := range []string{"existing", surface.MCPName, mcpName} {
				server, ok := config.Servers[name]
				if !ok || (server.Command == "" && server.URL == "") {
					t.Errorf("server %s lost its transport", name)
				}
			}
			if _, exists := config.Servers["mcpServers"]; exists {
				t.Fatal("MCP wrapper became a server with no transport")
			}
		})
	}
}

func TestCommandHarnessUsesTheSameScopedBotTools(t *testing.T) {
	s, _ := testService(t)
	var adapter agent.Adapter
	for _, a := range agent.All() {
		if a.ID() == "opencode" {
			adapter = a
		}
	}
	turn, cleanup, err := s.Bind(t.Context(), commandOnlyAdapter{adapter}, runner.Turn{ChatID: "chat"})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if !strings.Contains(turn.Options.ContextInstructions, "--bot-tool") || turn.Environment["MINDWIRE_BOT_TOOL_HELPER"] == "" {
		t.Fatal("command harness received no tool bridge")
	}
	t.Setenv("MINDWIRE_BOT_TOOL_URL", turn.Environment["MINDWIRE_BOT_TOOL_URL"])
	t.Setenv(tokenEnv, turn.Environment[tokenEnv])
	var output bytes.Buffer
	if err = ToolHelper(t.Context(), []string{"bots_create", `{"name":"Travel Planner","purpose":"Plan trips","instructions":"Ask about dates and budget"}`}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "mindwireBot") || strings.Contains(output.String(), turn.Environment[tokenEnv]) {
		t.Fatal("missing result or leaked token")
	}
	bots, err := s.db.Bots("project", false)
	if err != nil || len(bots) != 2 {
		t.Fatalf("helper did not persist bot: %+v %v", bots, err)
	}
	t.Setenv("MINDWIRE_BOT_TOOL_URL", "https://example.invalid/mcp")
	if err = ToolHelper(t.Context(), []string{"list"}, &output); err == nil {
		t.Fatal("helper accepted a remote endpoint")
	}
}

type commandOnlyAdapter struct{ agent.Adapter }

func (a commandOnlyAdapter) Capabilities() agent.Capabilities {
	caps := a.Adapter.Capabilities()
	caps.MCPServers = false
	return caps
}

func TestApplicationManagedBotPreservesCallerToolBoundary(t *testing.T) {
	s, master := testService(t)
	bot, err := s.db.CreateBot(registry.Bot{AgentID: "agent", ProjectID: "project", Name: "App investigator", Purpose: "Trace app failures", Instructions: "Use only approved app tools", ExternalTools: true})
	if err != nil {
		t.Fatal(err)
	}
	chat, err := s.db.CreateBotChat(bot.ID, "application-chat", "Investigate")
	if err != nil {
		t.Fatal(err)
	}
	for _, harness := range []string{"codex", "claude-code"} {
		t.Run(harness, func(t *testing.T) {
			var adapter agent.Adapter
			for _, candidate := range agent.All() {
				if candidate.ID() == harness {
					adapter = candidate
				}
			}
			original := runner.Turn{ChatID: chat.ID, Environment: map[string]string{"OPENSHIP_TASK": "scoped"}, Options: agent.TurnOptions{ContextInstructions: "Application policy.", MCPServers: json.RawMessage(`{"openship":{"url":"http://127.0.0.1/mcp"}}`)}}
			bound, cleanup, err := s.Bind(t.Context(), adapter, original)
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			if string(bound.Options.MCPServers) != string(original.Options.MCPServers) || len(bound.Environment) != 1 || bound.Environment["OPENSHIP_TASK"] != "scoped" {
				t.Fatal("application tool boundary was changed")
			}
			if !strings.HasPrefix(bound.Options.ContextInstructions, "Application policy.") || !strings.Contains(bound.Options.ContextInstructions, "App investigator") || !strings.Contains(bound.Options.ContextInstructions, bot.Instructions) {
				t.Fatal("lost application policy or specialist context")
			}
			if strings.Contains(bound.Options.ContextInstructions, "bots_update") || bound.Environment[tokenEnv] != "" || bound.Options.SkillsRoot != "" {
				t.Fatal("native management capability leaked into application task")
			}
		})
	}
	for _, actor := range []registry.Bot{{}, master, bot} {
		if _, err := s.taskTarget(actor, bot.ID, true); err == nil {
			t.Fatal("application task was executable through native delegation")
		}
	}
	archived := true
	if _, err = s.db.UpdateBot(bot.ID, registry.BotPatch{ExpectedRevision: bot.Revision, Archived: &archived}); err != nil {
		t.Fatal(err)
	}
	var adapter agent.Adapter
	for _, candidate := range agent.All() {
		if candidate.ID() == "codex" {
			adapter = candidate
		}
	}
	if _, _, err := s.Bind(t.Context(), adapter, runner.Turn{ChatID: chat.ID}); err == nil {
		t.Fatal("archived application agent accepted a turn")
	}
}
