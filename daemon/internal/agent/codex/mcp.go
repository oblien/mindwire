package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Persistent MCP-server config for Codex, exposed through the optional agent.MCPServerModule
// (type-asserted by the API, never part of the mandatory Adapter). Codex reads MCP servers from
// `$CODEX_HOME/config.toml` `[mcp_servers.NAME]` tables — user scope only (Codex has no project-scope
// config convention). This is DISTINCT from the per-turn --mcp-config overlay in mcpconfig.go: here we
// read/write the base config the CLI loads on every run.
//
// The writer REUSES the hand-rolled TOML emitter from mcpconfig.go (buildConfigOverlay). The reader is a
// deliberately minimal, zero-dependency TOML scanner that understands ONLY the `[mcp_servers.*]` tables
// it manages; every other table and key in config.toml is left byte-for-byte intact (Set/Delete are a
// surgical remove-then-append on those sections). No mindwire secret is ever written — an MCPServer
// carries a bearer-token env-var NAME, never a value.
var _ agent.MCPServerModule = adapter{}

// mcpConfigPath is Codex's single persistent config file. Empty when the home dir can't be resolved.
func mcpConfigPath() string {
	base := configBase()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "config.toml")
}

// MCPScopes: Codex's persistent MCP config is user-scope only.
func (adapter) MCPScopes() []agent.MemoryScope { return []agent.MemoryScope{agent.MemoryUser} }

// ListMCPServers parses every `[mcp_servers.*]` table from config.toml. A missing file yields an empty
// map (forgiving). dir is ignored — Codex is user-only. An unsupported scope is an error.
func (adapter) ListMCPServers(scope agent.MemoryScope, _ string) (map[string]agent.MCPServer, error) {
	if scope != agent.MemoryUser {
		return nil, fmt.Errorf("codex supports MCP config only at user scope")
	}
	path := mcpConfigPath()
	if path == "" {
		return nil, fmt.Errorf("cannot resolve codex home")
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]agent.MCPServer{}, nil
	}
	if err != nil {
		return nil, err
	}
	return parseMCPServers(string(data)), nil
}

// SetMCPServer writes one server, preserving all sibling config: it removes any existing
// `[mcp_servers.NAME]` section (and its .env / .http_headers sub-tables), then appends a freshly
// emitted section. Order of top-level tables is TOML-insignificant, so appending is safe.
func (adapter) SetMCPServer(scope agent.MemoryScope, _ string, name string, server agent.MCPServer) error {
	if scope != agent.MemoryUser {
		return fmt.Errorf("codex supports MCP config only at user scope")
	}
	if err := agent.ValidatePromptName(name); err != nil {
		return err
	}
	path := mcpConfigPath()
	if path == "" {
		return fmt.Errorf("cannot resolve codex home")
	}
	existing, err := readConfig(path)
	if err != nil {
		return err
	}
	body := strings.Join(removeServer(splitLines(existing), name), "\n")
	body = strings.TrimRight(body, "\n")
	section := strings.TrimLeft(buildConfigOverlay("", map[string]codexMCPServer{name: server}), "\n")
	var out string
	if strings.TrimSpace(body) == "" {
		out = section
	} else {
		out = body + "\n\n" + section
	}
	out = strings.TrimRight(out, "\n") + "\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("prepare codex home: %w", err)
	}
	return os.WriteFile(path, []byte(out), 0o600)
}

// DeleteMCPServer removes one server's section(s). Removing an absent server (or a missing file) is not
// an error — idempotent.
func (adapter) DeleteMCPServer(scope agent.MemoryScope, _ string, name string) error {
	if scope != agent.MemoryUser {
		return fmt.Errorf("codex supports MCP config only at user scope")
	}
	path := mcpConfigPath()
	if path == "" {
		return fmt.Errorf("cannot resolve codex home")
	}
	existing, err := readConfig(path)
	if err != nil {
		return err
	}
	if strings.TrimSpace(existing) == "" {
		return nil
	}
	out := strings.TrimRight(strings.Join(removeServer(splitLines(existing), name), "\n"), "\n")
	if out != "" {
		out += "\n"
	}
	return os.WriteFile(path, []byte(out), 0o600)
}

// readConfig reads config.toml, treating a missing file as empty content (not an error).
func readConfig(path string) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// ---- minimal TOML reader (only the [mcp_servers.*] tables) -----------------

// parseMCPServers scans config.toml and returns the servers under `[mcp_servers.NAME]` tables (plus
// their `.env` / `.http_headers` sub-tables). Every other table is ignored. Best-effort and
// zero-dependency: it understands flat `key = value` lines with basic/literal strings and inline string
// arrays — exactly what the emitter writes.
func parseMCPServers(content string) map[string]agent.MCPServer {
	out := map[string]agent.MCPServer{}
	var curName, curSub string
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if strings.HasPrefix(t, "[") {
			curName, curSub = "", ""
			segs, ok := parseTableHeader(t)
			if !ok || len(segs) < 2 || segs[0] != "mcp_servers" {
				continue
			}
			curName = segs[1]
			if _, exists := out[curName]; !exists {
				out[curName] = agent.MCPServer{}
			}
			if len(segs) >= 3 {
				curSub = segs[2]
			}
			continue
		}
		if curName == "" {
			continue
		}
		key, val, ok := splitKV(t)
		if !ok {
			continue
		}
		s := out[curName]
		switch curSub {
		case "":
			switch key {
			case "command":
				s.Command = parseTOMLValue(val)
			case "args":
				s.Args = parseTOMLArray(val)
			case "cwd":
				s.Cwd = parseTOMLValue(val)
			case "url":
				s.URL = parseTOMLValue(val)
			case "bearer_token_env_var":
				s.BearerTokenEnvVar = parseTOMLValue(val)
			}
		case "env":
			if s.Env == nil {
				s.Env = map[string]string{}
			}
			s.Env[key] = parseTOMLValue(val)
		case "http_headers":
			if s.HTTPHeaders == nil {
				s.HTTPHeaders = map[string]string{}
			}
			s.HTTPHeaders[key] = parseTOMLValue(val)
		}
		out[curName] = s
	}
	return out
}

// removeServer drops every table belonging to server `name` — `[mcp_servers.name]` and its
// `[mcp_servers.name.*]` sub-tables — plus a single blank separator line immediately preceding each
// removed table. All other lines (sibling tables, top-level keys, comments) are preserved verbatim.
func removeServer(lines []string, name string) []string {
	out := make([]string, 0, len(lines))
	skip := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			segs, ok := parseTableHeader(t)
			if ok && len(segs) >= 2 && segs[0] == "mcp_servers" && segs[1] == name {
				// Drop the blank separator we would have emitted before this section.
				if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
					out = out[:len(out)-1]
				}
				skip = true
				continue
			}
			skip = false // any other table header ends the removed span
		}
		if skip {
			continue
		}
		out = append(out, line)
	}
	return out
}

// parseTableHeader parses a single-line TOML table header (e.g. [mcp_servers."my srv".env]) into its
// dotted, unquoted segments. Returns ok=false for an array-of-tables ([[...]]) or a malformed header. It
// scans respecting quoted segments, so a '.' or ']' inside a quoted key is not treated as a delimiter.
func parseTableHeader(line string) ([]string, bool) {
	if !strings.HasPrefix(line, "[") || strings.HasPrefix(line, "[[") {
		return nil, false
	}
	var segs []string
	var cur strings.Builder
	var segQuote byte // 0 bare, '"' basic, '\'' literal
	inStr := false
	var q byte
	closed := false
	for i := 1; i < len(line); i++ {
		c := line[i]
		if inStr {
			if q == '"' && c == '\\' && i+1 < len(line) {
				cur.WriteByte(c)
				cur.WriteByte(line[i+1])
				i++
				continue
			}
			if c == q {
				inStr = false
				continue
			}
			cur.WriteByte(c)
			continue
		}
		switch c {
		case '"', '\'':
			inStr, q, segQuote = true, c, c
		case '.':
			segs = append(segs, finishHeaderSeg(cur.String(), segQuote))
			cur.Reset()
			segQuote = 0
		case ']':
			segs = append(segs, finishHeaderSeg(cur.String(), segQuote))
			closed = true
			i = len(line)
		default:
			cur.WriteByte(c)
		}
	}
	if !closed || len(segs) == 0 {
		return nil, false
	}
	return segs, true
}

// finishHeaderSeg finalizes one header segment: a basic-string segment is escape-decoded, a literal
// segment is taken verbatim, a bare segment is space-trimmed.
func finishHeaderSeg(raw string, quote byte) string {
	switch quote {
	case '"':
		return decodeBasicString(raw)
	case '\'':
		return raw
	default:
		return strings.TrimSpace(raw)
	}
}

// splitKV splits a `key = value` line at the first '=' outside a quoted key, returning the (unquoted)
// key and the raw value token. Handles a quoted key (env/header names our emitter quotes when they
// aren't safe identifiers).
func splitKV(line string) (key, val string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", false
	}
	if line[0] == '"' || line[0] == '\'' {
		q := line[0]
		i := 1
		for i < len(line) {
			if q == '"' && line[i] == '\\' && i+1 < len(line) {
				i += 2
				continue
			}
			if line[i] == q {
				break
			}
			i++
		}
		if i >= len(line) {
			return "", "", false
		}
		inner := line[1:i]
		if q == '"' {
			inner = decodeBasicString(inner)
		}
		rest := strings.TrimSpace(line[i+1:])
		if !strings.HasPrefix(rest, "=") {
			return "", "", false
		}
		return inner, strings.TrimSpace(rest[1:]), true
	}
	eq := strings.IndexByte(line, '=')
	if eq < 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:eq]), strings.TrimSpace(line[eq+1:]), true
}

// parseTOMLValue reads a scalar value token into a Go string: a basic string ("...") is escape-decoded,
// a literal string ('...') is verbatim, anything else (bare token) has a trailing comment stripped and
// is returned as-is. Only string-typed fields are read, so this covers our domain.
func parseTOMLValue(tok string) string {
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return ""
	}
	switch tok[0] {
	case '"':
		inner, _ := scanQuoted(tok, '"')
		return decodeBasicString(inner)
	case '\'':
		inner, _ := scanQuoted(tok, '\'')
		return inner
	default:
		if h := strings.IndexByte(tok, '#'); h >= 0 {
			tok = strings.TrimSpace(tok[:h])
		}
		return tok
	}
}

// parseTOMLArray reads an inline `["a", "b"]` array of strings. Non-string elements are skipped. A
// multi-line array (uncommon, and never emitted by us) is read only up to the end of this line.
func parseTOMLArray(tok string) []string {
	tok = strings.TrimSpace(tok)
	if !strings.HasPrefix(tok, "[") {
		return nil
	}
	inner := tok[1:]
	if end := strings.LastIndexByte(inner, ']'); end >= 0 {
		inner = inner[:end]
	}
	var out []string
	i := 0
	for i < len(inner) {
		c := inner[i]
		switch {
		case c == ' ' || c == '\t' || c == ',':
			i++
		case c == '"' || c == '\'':
			s, n := scanQuoted(inner[i:], c)
			if c == '"' {
				s = decodeBasicString(s)
			}
			out = append(out, s)
			i += n
		default:
			i++ // skip an unexpected (non-string) element char
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// scanQuoted reads a quoted string that begins at s[0]==q and returns its inner content (without the
// quotes) and the number of bytes consumed including both quotes. For a basic string ('"') backslash
// escapes are kept raw (decodeBasicString handles them); a literal string is taken verbatim.
func scanQuoted(s string, q byte) (inner string, consumed int) {
	i := 1
	var b strings.Builder
	for i < len(s) {
		c := s[i]
		if q == '"' && c == '\\' && i+1 < len(s) {
			b.WriteByte(c)
			b.WriteByte(s[i+1])
			i += 2
			continue
		}
		if c == q {
			return b.String(), i + 1
		}
		b.WriteByte(c)
		i++
	}
	return b.String(), len(s) // unterminated — take what we have
}

// decodeBasicString reverses tomlString: it decodes the escapes a TOML basic string can carry. Unknown
// escapes are passed through as their literal character (best-effort).
func decodeBasicString(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		case 'u':
			if i+4 < len(s) {
				if r := decodeHex4(s[i+1 : i+5]); r >= 0 {
					b.WriteRune(rune(r))
					i += 4
					continue
				}
			}
			b.WriteByte('u')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// decodeHex4 parses exactly four hex digits into a code point, or -1 if they aren't all hex.
func decodeHex4(s string) int {
	if len(s) != 4 {
		return -1
	}
	v := 0
	for i := 0; i < 4; i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			v = v<<4 | int(c-'0')
		case c >= 'a' && c <= 'f':
			v = v<<4 | int(c-'a'+10)
		case c >= 'A' && c <= 'F':
			v = v<<4 | int(c-'A'+10)
		default:
			return -1
		}
	}
	return v
}
