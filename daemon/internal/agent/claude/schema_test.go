package claude

import (
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// The adapter's settings schema must satisfy the taxonomy invariants: every field is scoped, every
// unified field carries a registered canon, and canons are unique. A mis-annotated claudeSpec fails
// here rather than on a live turn.
func TestSettingsSchemaTaxonomy(t *testing.T) {
	if err := agent.ValidateSchema(adapter{}.Settings()); err != nil {
		t.Fatalf("claude settings schema violates taxonomy: %v", err)
	}
}
