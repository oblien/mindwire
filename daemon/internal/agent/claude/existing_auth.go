package claude

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func (m *authModule) SetAuthSource(source *agent.AuthSource) { m.source = source }

func (m *authModule) ExistingMethod() *agent.AuthMethod {
	_, found, _ := m.sourceAccount(context.Background(), false)
	if !found {
		return nil
	}
	return &agent.AuthMethod{ID: agent.ExistingAuthMethod, Label: "Use existing connection", Scope: agent.ScopeUnified,
		Help: "Use the Claude account already configured on this computer.", Existing: &agent.ExistingAuthConnection{Name: "Claude account"}}
}

func (m *authModule) UseExisting(ctx context.Context) (agent.AuthState, error) {
	fail := func(message string) (agent.AuthState, error) {
		return agent.AuthState{Method: agent.ExistingAuthMethod, Status: "error", Message: message}, nil
	}
	data, found, err := m.sourceAccount(ctx, true)
	if err != nil {
		return fail("Could not read the saved Claude connection. Unlock your keychain or choose another method.")
	}
	if !found {
		return fail("No saved Claude account was found. Scan again or choose a sign-in method.")
	}
	m.cancelLogin()
	err = agent.ImportNativeAuth(ctx, m.store, map[string][]byte{filepath.Join(configBase(), ".credentials.json"): data}, agent.NativeAuthSettings(m, "login"),
		func(ctx context.Context) bool { return m.Status(ctx).Configured })
	if err != nil {
		return fail("The saved Claude connection could not be verified. Sign in again or choose another method.")
	}
	return agent.AuthState{Method: agent.ExistingAuthMethod, Status: "complete"}, nil
}

func (m *authModule) sourceAccount(ctx context.Context, reveal bool) ([]byte, bool, error) {
	if m.source == nil {
		return nil, false, nil
	}
	directory := m.source.ConfigDir("CLAUDE_CONFIG_DIR", ".claude")
	if directory == "" || filepath.Clean(directory) == filepath.Clean(configBase()) {
		return nil, false, nil
	}
	raw, err := agent.ReadNativeAuthFile(filepath.Join(directory, ".credentials.json"))
	if err != nil {
		return nil, false, err
	}
	if data := sourceAccountJSON(raw); data != nil {
		return data, true, nil
	}
	service := "Claude Code"
	if directory != filepath.Join(m.source.Home, ".claude") {
		sum := sha256.Sum256([]byte(directory))
		service += "-" + hex.EncodeToString(sum[:])[:8]
	}
	raw, found, err := m.source.ReadKeychain(ctx, service+"-credentials", "", reveal)
	if err != nil || !found {
		return nil, false, err
	}
	if !reveal {
		return nil, true, nil
	}
	data := sourceAccountJSON(raw)
	return data, data != nil, nil
}

func sourceAccountJSON(raw []byte) []byte {
	var account struct {
		OAuth map[string]json.RawMessage `json:"claudeAiOauth"`
	}
	if json.Unmarshal(raw, &account) != nil {
		return nil
	}
	var token string
	if json.Unmarshal(account.OAuth["accessToken"], &token) != nil || token == "" {
		return nil
	}
	allowed := map[string]json.RawMessage{}
	for _, key := range []string{"accessToken", "refreshToken", "expiresAt", "scopes", "subscriptionType", "rateLimitTier"} {
		if value := account.OAuth[key]; value != nil {
			allowed[key] = value
		}
	}
	data, _ := json.Marshal(map[string]any{"claudeAiOauth": allowed})
	return data
}
