package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/proc"
)

// This file makes Claude's settings VALUES come from the CLI, not hardcoded constants.
// The adapter owns the references (which flags, field types, labels); the CLI owns the
// option lists: enum choices are parsed from `claude --help`, and the built-in tool set
// is learned from the runtime system/init event.

// ---- tool set: learned from system/init events ------------------------------------

var (
	toolMu   sync.Mutex
	toolSeen = map[string]bool{}
)

// rememberTools records tool names Claude reported. It UNIONs across runs, so a turn
// that restricts --tools only ever adds names, never shrinks the known set.
func rememberTools(names []string) {
	toolMu.Lock()
	defer toolMu.Unlock()
	for _, n := range names {
		if n != "" {
			toolSeen[n] = true
		}
	}
}

// knownTools returns the sorted union of tool names Claude has reported. Empty until a
// turn has surfaced an init event.
func knownTools() []string {
	toolMu.Lock()
	defer toolMu.Unlock()
	out := make([]string, 0, len(toolSeen))
	for n := range toolSeen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ---- flag choices: parsed from `claude --help` (the source of truth) ---------------

// claude --help is cached with a TTL (see agent.HelpCache): one exec per window (so opening the
// settings UI repeatedly doesn't shell out each time), refreshed periodically so a CLI upgrade (e.g.
// via POST /update) is picked up without a daemon restart.
var claudeHelp = &agent.HelpCache{Command: "claude", Args: []string{"--help"}}

// choicesFor returns the values the CLI enumerates for a flag, discovered from
// `claude --help`. nil if the flag isn't found or declares no choices.
func choicesFor(flag string) []string { return parseChoices(claudeHelp.Get(), flag) }

// parseChoices extracts a flag's enumerated values from help text. Help wraps across
// lines, so collapse whitespace, isolate the flag's blurb (up to the next option), then
// read either a (choices: "a", "b", …) list or a bare (w1, w2, …) list of lowercase
// words. The bare-word guard is what keeps --model's "(e.g. 'opus', 'sonnet')" example
// from being mistaken for an enum (so model correctly stays a free-text field).
func parseChoices(help, flag string) []string {
	h := strings.Join(strings.Fields(help), " ")
	i := strings.Index(h, " "+flag+" ")
	if i < 0 {
		return nil
	}
	blurb := h[i+len(flag)+2:]
	if j := strings.Index(blurb, " --"); j >= 0 {
		blurb = blurb[:j]
	}
	if a := strings.Index(blurb, "(choices:"); a >= 0 {
		if b := strings.Index(blurb[a:], ")"); b >= 0 {
			return agent.ParseCSV(blurb[a+len("(choices:") : a+b])
		}
	}
	rest := blurb
	for {
		a := strings.Index(rest, "(")
		if a < 0 {
			break
		}
		b := strings.Index(rest[a:], ")")
		if b < 0 {
			break
		}
		items := agent.ParseCSV(rest[a+1 : a+b])
		if len(items) >= 2 && allBareWords(items) {
			return items
		}
		rest = rest[a+b+1:]
	}
	return nil
}

func allBareWords(items []string) bool {
	for _, w := range items {
		if w == "" {
			return false
		}
		for _, r := range w {
			if (r < 'a' || r > 'z') && r != '-' {
				return false
			}
		}
	}
	return true
}

// ---- model list: the Anthropic Models API (the CLI has no list command) ------------
// The `claude` CLI exposes no scriptable model list, so the real programmatic source of
// truth is GET /v1/models. We fetch it with the credential the user configured (so it's
// the actual list available to THIS account), cache it with a TTL, and the model field
// becomes a real picker. No creds / offline / error → the cache stays empty and the
// field degrades to free text (still CLI-validated). Nothing is hardcoded.

const (
	modelTTL   = 30 * time.Minute // how long a good list stays fresh
	modelRetry = 2 * time.Minute  // negative-result backoff: don't re-hit the API after a miss
)

// ModelOpt is one available model: its API id + a human label.
type ModelOpt struct {
	ID    string
	Label string
}

var (
	modelMu      sync.Mutex
	modelOpts    []ModelOpt
	modelAt      time.Time // last SUCCESSFUL fetch
	modelTriedAt time.Time // last ATTEMPT (success or failure) — drives the backoff
	modelBusy    bool
)

func knownModels() []ModelOpt {
	modelMu.Lock()
	defer modelMu.Unlock()
	return append([]ModelOpt(nil), modelOpts...)
}

// ---- host-level credential capture (external auth) --------------------------------
//
// Claude Code's auth is frequently HOST-LEVEL: an ANTHROPIC_API_KEY (or OAuth / gateway bearer token)
// exported in the user's shell profile and resolved by the CLI itself — never stored in the daemon.
// `claude auth status` already trusts that resolution (it runs through a `bash -lc` login shell), so
// "Signed in" is reported while the daemon cred store is empty. A real turn authenticates the same way
// (driver runs `bash -lc` with os.Environ() + EnvForRun). The models fetch must trust the SAME source,
// or the picker stays empty while the agent is plainly signed in.
//
// hostCredVars are the credential + request-shaping vars the fetch understands — the SAME set Claude
// Code itself resolves. Besides the credential, a gateway needs its endpoint (ANTHROPIC_BASE_URL), its
// workspace (ANTHROPIC_WORKSPACE_ID → the anthropic-workspace-id header) and any ANTHROPIC_CUSTOM_HEADERS:
// without those the CLI's own /models works but a bare key call is rejected. We capture just these (a
// targeted read, never a full-env dump). It's a package var so tests can stub it without shelling out.
var hostCredVars = []string{
	"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_AUTH_TOKEN",
	"ANTHROPIC_BASE_URL", "ANTHROPIC_WORKSPACE_ID", "ANTHROPIC_CUSTOM_HEADERS",
}

var hostCredEnv = func() map[string]string {
	got := loginShellCreds()
	if got == nil {
		got = map[string]string{}
	}
	// settings.json's `env` block is the CLI's own authoritative source (a user may set these there and
	// NOT export them to the shell). Overlay it so the daemon resolves exactly as `claude` does.
	for k, v := range claudeSettingsEnv() {
		got[k] = v
	}
	return got
}

// loginShellCreds reads hostCredVars from a LOGIN shell — the same resolution the auth check and a real
// turn use, so profile exports resolve identically even though the daemon may be launched with a
// stripped env. Values are NUL-delimited (env values can't contain NUL) so a multi-line
// ANTHROPIC_CUSTOM_HEADERS survives intact; the printf arg list is built from hostCredVars so the two
// never drift.
func loginShellCreds() map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), authStatusTimeout)
	defer cancel()
	var sb strings.Builder
	sb.WriteString(`printf '%s\0'`)
	for _, name := range hostCredVars { // names are fixed [A-Z_] constants → no injection
		sb.WriteString(` "$`)
		sb.WriteString(name)
		sb.WriteString(`"`)
	}
	cmd := exec.CommandContext(ctx, "bash", "-lc", sb.String())
	proc.Group(cmd) // timeout kills the whole shell tree
	cmd.Env = os.Environ()
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if cmd.Run() != nil {
		return nil // best-effort: a failure just leaves nothing to add
	}
	parts := bytes.Split(stdout.Bytes(), []byte{0})
	got := map[string]string{}
	for i, name := range hostCredVars {
		if i < len(parts) {
			if v := strings.TrimSpace(string(parts[i])); v != "" {
				got[name] = v
			}
		}
	}
	return got
}

// claudeSettingsEnv reads the credential/header vars from ~/.claude/settings.json's `env` block — where
// Claude Code stores gateway config (ANTHROPIC_BASE_URL / ANTHROPIC_WORKSPACE_ID / ANTHROPIC_CUSTOM_HEADERS).
// Best-effort: a missing or malformed file yields nothing.
func claudeSettingsEnv() map[string]string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		return nil
	}
	var s struct {
		Env map[string]string `json:"env"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return nil
	}
	out := map[string]string{}
	for _, name := range hostCredVars {
		if v := strings.TrimSpace(s.Env[name]); v != "" {
			out[name] = v
		}
	}
	return out
}

// applyCustomHeaders sets the headers declared in ANTHROPIC_CUSTOM_HEADERS (Claude Code's convention:
// one `Name: Value` per line; we also tolerate literal `\n` escapes). Applied after the well-known
// headers so an explicit override wins.
func applyCustomHeaders(req *http.Request, raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	raw = strings.ReplaceAll(raw, `\n`, "\n")
	for _, line := range strings.Split(raw, "\n") {
		name, val, ok := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			continue
		}
		req.Header.Set(name, strings.TrimSpace(val))
	}
}

// resolveModelEnv fills first-party credential vars from the host login shell when the daemon holds no
// Claude credential of its own (the host-level / external-auth case). Daemon-configured creds win and
// are returned untouched; a cloud backend (Bedrock/Vertex/Foundry) owns the whole env, so we never
// graft a first-party key onto one (its models don't live at api.anthropic.com/v1/models anyway). This
// is the models-fetch analogue of the login-shell resolution the auth check already trusts.
func resolveModelEnv(env map[string]string) map[string]string {
	out := make(map[string]string, len(env))
	for k, v := range env {
		out[k] = v
		if strings.HasPrefix(k, "CLAUDE_CODE_USE_") && v != "" {
			return out // a cloud backend is selected; leave its env alone
		}
	}
	// Daemon already carries a usable first-party credential → trust it as-is, no host lookup.
	if out["ANTHROPIC_API_KEY"] != "" || out["CLAUDE_CODE_OAUTH_TOKEN"] != "" || out["ANTHROPIC_AUTH_TOKEN"] != "" {
		return out
	}
	for name, v := range hostCredEnv() {
		if out[name] == "" && v != "" {
			out[name] = v
		}
	}
	return out
}

// ensureModels refreshes the model list from /v1/models using whatever credential resolveModelEnv
// yields — the daemon-stored cred if present, else the host-level cred the CLI itself would use
// (ANTHROPIC_API_KEY → x-api-key; CLAUDE_CODE_OAUTH_TOKEN / ANTHROPIC_AUTH_TOKEN → Bearer; honoring
// ANTHROPIC_BASE_URL for a gateway). TTL-guarded + single-flight; best-effort, so a failure leaves the
// cache unchanged. Safe to call concurrently (e.g. from a goroutine) on every /agent.
func ensureModels(env map[string]string) {
	modelMu.Lock()
	fresh := len(modelOpts) > 0 && time.Since(modelAt) < modelTTL
	backoff := !modelTriedAt.IsZero() && time.Since(modelTriedAt) < modelRetry
	if fresh || backoff || modelBusy {
		modelMu.Unlock()
		return
	}
	modelBusy = true
	modelTriedAt = time.Now() // record the attempt now so failures back off, not just successes
	modelMu.Unlock()
	defer func() { modelMu.Lock(); modelBusy = false; modelMu.Unlock() }()

	// Resolve credentials the way a real turn does — only now that we've committed to a fetch (cache
	// miss, not in backoff), so the host login-shell read happens at most once per refresh window.
	env = resolveModelEnv(env)

	base := "https://api.anthropic.com"
	if u := strings.TrimSpace(env["ANTHROPIC_BASE_URL"]); u != "" {
		base = strings.TrimRight(u, "/")
	}
	req, err := http.NewRequest(http.MethodGet, base+"/v1/models?limit=100", nil)
	if err != nil {
		return
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	switch {
	case env["ANTHROPIC_API_KEY"] != "":
		req.Header.Set("x-api-key", env["ANTHROPIC_API_KEY"])
	case env["CLAUDE_CODE_OAUTH_TOKEN"] != "":
		req.Header.Set("Authorization", "Bearer "+env["CLAUDE_CODE_OAUTH_TOKEN"])
	case env["ANTHROPIC_AUTH_TOKEN"] != "":
		req.Header.Set("Authorization", "Bearer "+env["ANTHROPIC_AUTH_TOKEN"])
	default:
		return // no credential anywhere (daemon or host) → nothing to fetch
	}
	// A workspace-scoped gateway rejects /v1/models without its workspace header; the CLI sends it from
	// ANTHROPIC_WORKSPACE_ID. ANTHROPIC_CUSTOM_HEADERS carries any other headers the endpoint requires
	// (applied last so it can override the well-known ones) — matching the CLI's own request exactly.
	if ws := strings.TrimSpace(env["ANTHROPIC_WORKSPACE_ID"]); ws != "" {
		req.Header.Set("anthropic-workspace-id", ws)
	}
	applyCustomHeaders(req, env["ANTHROPIC_CUSTOM_HEADERS"])
	resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var body struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil || len(body.Data) == 0 {
		return
	}
	opts := make([]ModelOpt, 0, len(body.Data))
	for _, m := range body.Data {
		label := m.DisplayName
		if label == "" {
			label = m.ID
		}
		opts = append(opts, ModelOpt{ID: m.ID, Label: label})
	}
	modelMu.Lock()
	modelOpts = opts
	modelAt = time.Now()
	modelMu.Unlock()
}
