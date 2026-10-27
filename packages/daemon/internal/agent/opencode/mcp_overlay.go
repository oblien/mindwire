package opencode

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/oblien/mindwire/packages/daemon/internal/agent"
)

// OpenCode merges this process-scoped overlay with its native config on startup.
// Recreating the server on resume refreshes product endpoints and credentials.
func applyMCPOverlay(env map[string]string, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var servers map[string]agent.MCPServer
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return err
	}
	if wrapped, ok := top["mcpServers"]; ok {
		raw = wrapped
	}
	if err := json.Unmarshal(raw, &servers); err != nil {
		return err
	}
	config := map[string]json.RawMessage{}
	existing := env["OPENCODE_CONFIG_CONTENT"]
	if existing == "" {
		existing = os.Getenv("OPENCODE_CONFIG_CONTENT")
	}
	if existing != "" {
		if err := json.Unmarshal([]byte(existing), &config); err != nil {
			return fmt.Errorf("invalid OPENCODE_CONFIG_CONTENT: %w", err)
		}
	}
	mcp := map[string]json.RawMessage{}
	if raw, ok := config["mcp"]; ok {
		if err := json.Unmarshal(raw, &mcp); err != nil {
			return err
		}
	}
	for name, server := range servers {
		if server.Command == "" && server.URL == "" {
			return fmt.Errorf("MCP server %s requires a command or URL", name)
		}
		mcp[name], _ = json.Marshal(toOpencode(server))
	}
	config["mcp"], _ = json.Marshal(mcp)
	encoded, err := json.Marshal(config)
	if err != nil {
		return err
	}
	env["OPENCODE_CONFIG_CONTENT"] = string(encoded)
	return nil
}
