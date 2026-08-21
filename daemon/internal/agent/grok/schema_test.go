package grok

import (
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// The settings schema is the public canonical contract. Keep it under the
// same conformance guard as Claude, Codex, and opencode so a future Grok field
// cannot bypass the unified vocabulary.
func TestSettingsSchemaIsCanonical(t *testing.T) {
	if err := agent.ValidateSchema(adapter{}.Settings()); err != nil {
		t.Fatal(err)
	}
}
