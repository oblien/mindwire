package codex

import (
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// The adapter's settings schema must satisfy the taxonomy invariants: every field is scoped, every
// unified field carries a registered canon, and canons are unique. A mis-annotated codexSpec fails
// here rather than on a live turn.
func TestSettingsSchemaTaxonomy(t *testing.T) {
	if err := agent.ValidateSchema(adapter{}.Settings()); err != nil {
		t.Fatalf("codex settings schema violates taxonomy: %v", err)
	}
}

// Security invariant: no auth credential key is a declared SETTING, so a secret can never leak into a
// turn's Config or be overwritten by a per-turn option — credentials reach the process ONLY via
// EnvForRun.
func TestAuthKeysNotSettings(t *testing.T) {
	keys := agent.SettingsKeys(adapter{}.Settings())
	for _, k := range []string{ckAPIKey, ckAccessToken, ckBaseURL, ckOrg, ckProject} {
		if keys[k] {
			t.Errorf("auth key %q is a declared setting; it must live only in the auth lane", k)
		}
	}
}
