package grok

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Grok owns its MCP config and OAuth credential store. Use its documented CLI
// rather than editing config.toml: this preserves its native validation and
// OAuth flows. The CLI documents reliable CRUD for its user-level store. Project
// config is intentionally not exposed here until Grok provides scope-aware list
// and remove commands; advertising partial CRUD would violate MCPServerModule.
var _ agent.MCPServerModule = adapter{}

func (adapter) MCPScopes() []agent.MemoryScope {
	return []agent.MemoryScope{agent.MemoryUser}
}

func grokMCPArgs(scope agent.MemoryScope, args ...string) ([]string, error) {
	if scope != agent.MemoryUser {
		return nil, fmt.Errorf("Grok Build supports persistent MCP configuration only at user scope")
	}
	return append([]string{"mcp"}, args...), nil
}

func runGrokMCP(scope agent.MemoryScope, args ...string) ([]byte, error) {
	args, err := grokMCPArgs(scope, args...)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("grok", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("grok %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func (adapter) ListMCPServers(scope agent.MemoryScope, _ string) (map[string]agent.MCPServer, error) {
	out, err := runGrokMCP(scope, "list", "--json")
	if err != nil {
		return nil, err
	}
	return decodeMCPList(out)
}

func (adapter) SetMCPServer(scope agent.MemoryScope, _ string, name string, server agent.MCPServer) error {
	if err := agent.ValidatePromptName(name); err != nil {
		return err
	}
	// `grok mcp add` supports command/args and HTTP headers, but does not expose
	// a CLI flag for a server environment or cwd. Reject those fields instead of
	// writing a definition that looks valid but loses execution semantics.
	if len(server.Env) > 0 || server.Cwd != "" {
		return fmt.Errorf("Grok Build persistent MCP via the CLI does not support server env or cwd; declare this server in Grok's config.toml")
	}
	var args []string
	if server.URL != "" {
		args = []string{"add", "--transport", "http", name, server.URL}
		headerNames := make([]string, 0, len(server.HTTPHeaders))
		for k := range server.HTTPHeaders {
			headerNames = append(headerNames, k)
		}
		sort.Strings(headerNames)
		for _, k := range headerNames {
			v := server.HTTPHeaders[k]
			args = append(args, "--header", k+": "+v)
		}
		if server.BearerTokenEnvVar != "" && !hasAuthorizationHeader(server.HTTPHeaders) {
			args = append(args, "--header", "Authorization: Bearer ${"+server.BearerTokenEnvVar+"}")
		}
	} else if server.Command != "" {
		args = append([]string{"add", name, "--", server.Command}, server.Args...)
	} else {
		return fmt.Errorf("grok MCP server requires command or url")
	}
	_, err := runGrokMCP(scope, args...)
	return err
}

func hasAuthorizationHeader(headers map[string]string) bool {
	for key := range headers {
		if strings.EqualFold(key, "authorization") {
			return true
		}
	}
	return false
}

func (adapter) DeleteMCPServer(scope agent.MemoryScope, _ string, name string) error {
	if err := agent.ValidatePromptName(name); err != nil {
		return err
	}
	_, err := runGrokMCP(scope, "remove", name)
	return err
}

// decodeMCPList accepts both shapes emitted across Grok Build releases: an object
// keyed by server name, or {servers:[{name,...}]}. Unknown fields deliberately
// survive as omission; the canonical server shape carries only portable fields.
func decodeMCPList(data []byte) (map[string]agent.MCPServer, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if raw, ok := root["servers"]; ok {
		var list []struct {
			Name    string            `json:"name"`
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		}
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, err
		}
		out := make(map[string]agent.MCPServer, len(list))
		for _, s := range list {
			if s.Name != "" {
				out[s.Name] = agent.MCPServer{Command: s.Command, Args: s.Args, Env: s.Env, URL: s.URL, HTTPHeaders: s.Headers}
			}
		}
		return out, nil
	}
	out := make(map[string]agent.MCPServer, len(root))
	for name, raw := range root {
		var s struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		}
		if json.Unmarshal(raw, &s) == nil {
			out[name] = agent.MCPServer{Command: s.Command, Args: s.Args, Env: s.Env, URL: s.URL, HTTPHeaders: s.Headers}
		}
	}
	return out, nil
}
