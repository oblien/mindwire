package codex

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// mcpconfig.go materializes a turn's per-run codex config as a PROFILE OVERLAY — a file
// `$CODEX_HOME/<profile>.config.toml` that `codex exec -p <profile>` layers on top of the user's base
// config (`codex --help`: "Layer $CODEX_HOME/<name>.config.toml on top of the base user config"). The
// overlay carries two turn options that codex has no CLI flag for:
//
//   - systemPrompt → `model_instructions_file = "<abs path>"` ("Replacement for built-in instructions
//     instead of AGENTS.md" per the config reference; codex reads the file at session start — a missing
//     path is a hard error, so it is provably consumed, not a reserved no-op like `instructions`).
//   - mcpServers   → `[mcp_servers.NAME]` tables (command/args/env/cwd for stdio, url/bearer for HTTP).
//
// Why an overlay in the REAL CODEX_HOME (not a throwaway CODEX_HOME): auth is env-only here
// (EnvForRun), so CODEX_HOME location is irrelevant to auth — but rollout/session files live under
// `$CODEX_HOME/sessions/**` and History/findRollout read the real home, so a throwaway home would
// strand this turn's rollout and break resume/History. Layering an overlay keeps sessions, auth, and
// base config intact. The overlay is written 0600 (it may hold client-provided MCP env) and removed by
// the turn's cleanup. It holds NO mindwire auth secret — those stay env-only.
//
// Verified on codex-cli 0.146.0: `-p` is accepted before the (optional) `resume` subcommand on
// `codex exec` and the overlay's fields pass `--strict-config`; a fresh turn loads it and emits
// `thread.started`. The app-server transport does NOT accept `-p`, so systemPrompt/mcpServers there are
// rejected honestly (see RunStream) rather than silently dropped.

// codexMCPServer is the canonical agent.MCPServer under a codex-local alias. It is the subset of a
// `[mcp_servers.NAME]` table we transcode a client's MCP JSON into — stdio (command/args/env/cwd) and
// streamable-HTTP (url/bearerTokenEnvVar/httpHeaders) forms are both supported; unknown fields are
// ignored (forward-compatible). Aliasing (not redefining) keeps the per-turn overlay emitter below and
// the persistent /mcp store (mcp.go) on ONE type, so a server round-trips identically through both.
type codexMCPServer = agent.MCPServer

// decodeMCPServers parses the opaque per-turn MCP JSON into a name→server map. It accepts both the
// wrapped `{"mcpServers": {NAME: {…}}}` shape and the bare `{NAME: {…}}` map. Empty input yields no
// servers (not an error).
func decodeMCPServers(raw json.RawMessage) (map[string]codexMCPServer, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var wrap struct {
		MCPServers map[string]codexMCPServer `json:"mcpServers"`
	}
	if json.Unmarshal(raw, &wrap) == nil && len(wrap.MCPServers) > 0 {
		return wrap.MCPServers, nil
	}
	var bare map[string]codexMCPServer
	if err := json.Unmarshal(raw, &bare); err != nil {
		return nil, err
	}
	return bare, nil
}

// buildConfigOverlay renders the overlay TOML. Top-level keys (model_instructions_file) MUST precede
// any [table] header — TOML binds bare keys to the most recent table — so the instructions line is
// emitted first, then the mcp_servers tables in deterministic (sorted) order.
func buildConfigOverlay(sysPromptPath string, servers map[string]codexMCPServer) string {
	var b strings.Builder
	if sysPromptPath != "" {
		b.WriteString("model_instructions_file = " + tomlString(sysPromptPath) + "\n")
	}
	for _, name := range slices.Sorted(maps.Keys(servers)) {
		s := servers[name]
		hdr := "mcp_servers." + tomlKey(name)
		b.WriteString("\n[" + hdr + "]\n")
		if s.Command != "" {
			b.WriteString("command = " + tomlString(s.Command) + "\n")
		}
		if len(s.Args) > 0 {
			b.WriteString("args = " + tomlStringArray(s.Args) + "\n")
		}
		if s.Cwd != "" {
			b.WriteString("cwd = " + tomlString(s.Cwd) + "\n")
		}
		if s.URL != "" {
			b.WriteString("url = " + tomlString(s.URL) + "\n")
		}
		if s.BearerTokenEnvVar != "" {
			b.WriteString("bearer_token_env_var = " + tomlString(s.BearerTokenEnvVar) + "\n")
		}
		if len(s.Env) > 0 {
			b.WriteString("\n[" + hdr + ".env]\n")
			for _, k := range slices.Sorted(maps.Keys(s.Env)) {
				b.WriteString(tomlKey(k) + " = " + tomlString(s.Env[k]) + "\n")
			}
		}
		if len(s.HTTPHeaders) > 0 {
			b.WriteString("\n[" + hdr + ".http_headers]\n")
			for _, k := range slices.Sorted(maps.Keys(s.HTTPHeaders)) {
				b.WriteString(tomlKey(k) + " = " + tomlString(s.HTTPHeaders[k]) + "\n")
			}
		}
	}
	return b.String()
}

// writeConfigOverlay writes the overlay into base (the real CODEX_HOME) under a unique profile name and
// returns the profile name (for `-p`) and the file path (for cleanup). The name is random so concurrent
// turns never collide.
func writeConfigOverlay(base, sysPromptPath string, servers map[string]codexMCPServer) (profile, path string, err error) {
	buf := make([]byte, 8)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	profile = "mindwire-" + hex.EncodeToString(buf)
	if err = os.MkdirAll(base, 0o700); err != nil {
		return "", "", fmt.Errorf("prepare codex home: %w", err)
	}
	path = filepath.Join(base, profile+".config.toml")
	if err = os.WriteFile(path, []byte(buildConfigOverlay(sysPromptPath, servers)), 0o600); err != nil {
		return "", "", err
	}
	return profile, path, nil
}

// tomlString renders s as a TOML basic string with the required escapes.
func tomlString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// tomlStringArray renders a []string as a TOML inline array of basic strings.
func tomlStringArray(xs []string) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = tomlString(x)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// tomlKey renders a table/key segment: a bare key when it is a safe identifier, else a quoted string.
func tomlKey(k string) string {
	if k != "" && strings.IndexFunc(k, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-')
	}) == -1 {
		return k
	}
	return tomlString(k)
}
