package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

type sourceConnection struct {
	metadata agent.ExistingAuthConnection
	file     string
	data     []byte
	apiKey   string
}

func (m *authModule) SetAuthSource(source *agent.AuthSource) { m.source = source }

func (m *authModule) ExistingMethod() *agent.AuthMethod {
	connection, _ := m.sourceConnection(context.Background(), false)
	if connection == nil {
		return nil
	}
	return &agent.AuthMethod{ID: agent.ExistingAuthMethod, Label: "Use existing connection", Scope: agent.ScopeUnified,
		Help: "Use the Codex connection already configured on this computer.", Existing: &connection.metadata}
}

func (m *authModule) UseExisting(ctx context.Context) (agent.AuthState, error) {
	fail := func(message string) (agent.AuthState, error) {
		return agent.AuthState{Method: agent.ExistingAuthMethod, Status: "error", Message: message}, nil
	}
	connection, err := m.sourceConnection(ctx, true)
	if err != nil {
		return fail("Could not read the saved Codex connection. Unlock your keychain or choose another method.")
	}
	if connection == nil {
		return fail("No saved Codex connection was found. Scan again or choose a sign-in method.")
	}
	m.cancelLogin()
	if connection.apiKey != "" {
		state, err := m.Step(ctx, map[string]string{ckAPIKey: connection.apiKey,
			ckBaseURL: m.source.Environment["OPENAI_BASE_URL"], ckOrg: m.source.Environment["OPENAI_ORGANIZATION"], ckProject: m.source.Environment["OPENAI_PROJECT"]})
		state.Method = agent.ExistingAuthMethod
		return state, err
	}
	method := "login"
	if connection.file == "config.toml" {
		method = "nativeConfig"
		previous, err := agent.ReadNativeAuthFile(filepath.Join(configBase(), connection.file))
		if err != nil {
			return fail("Could not read this workspace's Codex configuration. Check its permissions and try again.")
		}
		connection.data = []byte(mergeSourceProviderConfig(string(previous), string(connection.data)))
	}
	settings := agent.NativeAuthSettings(m, method)
	if connection.metadata.Model != "" {
		settings[keyModel] = connection.metadata.Model
	}
	err = agent.ImportNativeAuth(ctx, m.store, map[string][]byte{filepath.Join(configBase(), connection.file): connection.data}, settings,
		func(ctx context.Context) bool { return m.Status(ctx).Configured })
	if err != nil {
		return fail("The saved Codex connection could not be verified. Sign in again or choose another method.")
	}
	return agent.AuthState{Method: agent.ExistingAuthMethod, Status: "complete"}, nil
}

func (m *authModule) sourceConnection(ctx context.Context, reveal bool) (*sourceConnection, error) {
	if m.source == nil {
		return nil, nil
	}
	directory := m.source.ConfigDir("CODEX_HOME", ".codex")
	if directory == "" || filepath.Clean(directory) == filepath.Clean(configBase()) {
		return nil, nil
	}
	config, err := agent.ReadNativeAuthFile(filepath.Join(directory, "config.toml"))
	if err != nil {
		return nil, err
	}
	id, model := nativeSelection(string(config))
	if id != "" && id != "openai" {
		data := sourceProviderConfig(string(config), m.source)
		if data == "" {
			return nil, nil
		}
		provider := parseModelProviders(string(config))[id]
		return &sourceConnection{metadata: agent.ExistingAuthConnection{Name: agent.FirstNonEmpty(provider.Name, id), Model: model}, file: "config.toml", data: []byte(data)}, nil
	}
	raw, err := agent.ReadNativeAuthFile(filepath.Join(directory, "auth.json"))
	if err != nil {
		return nil, err
	}
	if connection := sourceAccount(raw); connection != nil {
		return connection, nil
	}
	if key := agent.FirstNonEmpty(m.source.Environment["CODEX_API_KEY"], m.source.Environment["OPENAI_API_KEY"]); key != "" {
		return &sourceConnection{metadata: agent.ExistingAuthConnection{Name: "Codex API connection"}, apiKey: key}, nil
	}
	canonical, err := filepath.EvalSymlinks(directory)
	if err != nil {
		canonical = directory
	}
	sum := sha256.Sum256([]byte(canonical))
	raw, found, err := m.source.ReadKeychain(ctx, "Codex Auth", "cli|"+hex.EncodeToString(sum[:])[:16], reveal)
	if err != nil || !found {
		return nil, err
	}
	if !reveal {
		return &sourceConnection{metadata: agent.ExistingAuthConnection{Name: "Codex account"}}, nil
	}
	return sourceAccount(raw), nil
}

func sourceAccount(raw []byte) *sourceConnection {
	var account struct {
		APIKey      string            `json:"OPENAI_API_KEY"`
		Tokens      map[string]string `json:"tokens"`
		LastRefresh string            `json:"last_refresh"`
	}
	if json.Unmarshal(raw, &account) != nil {
		return nil
	}
	if key := strings.TrimSpace(account.APIKey); key != "" {
		return &sourceConnection{metadata: agent.ExistingAuthConnection{Name: "Codex API connection"}, apiKey: key}
	}
	if account.Tokens["access_token"] == "" || account.Tokens["refresh_token"] == "" {
		return nil
	}
	tokens := map[string]string{}
	for _, key := range []string{"id_token", "access_token", "refresh_token", "account_id"} {
		if value := account.Tokens[key]; value != "" {
			tokens[key] = value
		}
	}
	data, _ := json.Marshal(map[string]any{"auth_mode": "chatgpt", "OPENAI_API_KEY": nil, "tokens": tokens, "last_refresh": account.LastRefresh})
	return &sourceConnection{metadata: agent.ExistingAuthConnection{Name: "Codex account"}, file: "auth.json", data: data}
}

// Uses the same native selection and TOML reader as normal Codex routing. Only
// the selected connection crosses profiles; tools, hooks, policies and other providers do not.
func sourceProviderConfig(content string, source *agent.AuthSource) string {
	root := nativeRootSettings(content)
	id, model := root["model_provider"], root["model"]
	provider := parseModelProviders(content)[id]
	if id == "" || model == "" || provider.BaseURL == "" {
		return ""
	}
	values := map[string]string{}
	sections := map[string]map[string]string{}
	var current []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			current, _ = parseTableHeader(line)
			continue
		}
		if len(current) < 2 || current[0] != "model_providers" || current[1] != id {
			continue
		}
		key, value, ok := splitKV(line)
		if !ok {
			continue
		}
		if len(current) == 2 {
			values[key] = parseTOMLValue(value)
		}
		if len(current) == 3 && (current[2] == "http_headers" || current[2] == "env_http_headers" || current[2] == "query_params") {
			section := current[2]
			value = parseTOMLValue(value)
			if section == "env_http_headers" {
				section = "http_headers"
				value = source.Environment[value]
				if value == "" {
					return ""
				}
			}
			if sections[section] == nil {
				sections[section] = map[string]string{}
			}
			sections[section][key] = value
		}
	}
	token := agent.FirstNonEmpty(values["experimental_bearer_token"], source.Environment[provider.EnvVar])
	if token == "" || values["requires_openai_auth"] == "true" {
		return ""
	}
	var out strings.Builder
	out.WriteString("model_provider = " + tomlString(id) + "\nmodel = " + tomlString(model) + "\n")
	for _, key := range []string{"model_reasoning_effort", "model_reasoning_summary"} {
		if root[key] != "" {
			out.WriteString(key + " = " + tomlString(root[key]) + "\n")
		}
	}
	header := "model_providers." + tomlKey(id)
	out.WriteString("\n[" + header + "]\nname = " + tomlString(agent.FirstNonEmpty(provider.Name, id)) + "\nbase_url = " + tomlString(provider.BaseURL) + "\n")
	out.WriteString("wire_api = " + tomlString(agent.FirstNonEmpty(values["wire_api"], "responses")) + "\nexperimental_bearer_token = " + tomlString(token) + "\n")
	for _, key := range []string{"request_max_retries", "stream_max_retries", "stream_idle_timeout_ms", "supports_websockets", "requires_openai_auth"} {
		if value := values[key]; value != "" {
			// Re-emit only scalar booleans/numbers, never arbitrary TOML text.
			var scalar any
			if json.Unmarshal([]byte(value), &scalar) == nil {
				switch scalar.(type) {
				case bool, float64:
					out.WriteString(key + " = " + value + "\n")
				}
			}
		}
	}
	for _, section := range []string{"http_headers", "query_params"} {
		if len(sections[section]) == 0 {
			continue
		}
		out.WriteString("\n[" + header + "." + section + "]\n")
		for key, value := range sections[section] {
			out.WriteString(tomlKey(key) + " = " + tomlString(value) + "\n")
		}
	}
	return out.String()
}

// Replace just the selected connection while preserving the destination's tools,
// policies, unrelated providers and profiles. The source's profile is flattened;
// an existing destination profile still inherits the new connection from the root.
func mergeSourceProviderConfig(existing, connection string) string {
	root := nativeRootSettings(connection)
	profile := nativeRootSettings(existing)["profile"]
	lines := removeProvider(splitLines(existing), root["model_provider"])
	var kept []string
	selected := true
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			parts, ok := parseTableHeader(strings.TrimSpace(line))
			selected = ok && len(parts) == 2 && parts[0] == "profiles" && parts[1] == profile
		}
		if key, _, ok := splitKV(line); ok && selected && root[key] != "" {
			continue
		}
		kept = append(kept, line)
	}
	section := strings.Index(connection, "\n[")
	if section < 0 {
		return existing
	}
	return connection[:section] + "\n" + strings.Join(kept, "\n") + "\n" + connection[section:]
}
