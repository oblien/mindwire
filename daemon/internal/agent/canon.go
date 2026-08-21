package agent

import "fmt"

// Canonical keys — the stable, cross-agent vocabulary the CLIENT addresses regardless of which
// agent is selected. A unified Field carries one of these in Field.Canon; the adapter maps it to
// its own Field.Key (its internal/flag-aligned name). Per-turn options are addressed by canon and
// resolved canon→key in the runner (the single security choke point), so a client can say
// "reasoningEffort" and have it land on whichever flag the selected agent uses.
//
// This registry is a CLOSED vocabulary: adding a unified concept means adding a constant here (a
// daemon build), never inventing one at the edge. Agent-specific concepts do NOT get a canon —
// they are custom fields (Scope==ScopeCustom) whose Canon conventionally equals their Key.
const (
	CanonModel              = "model"
	CanonFallbackModel      = "fallbackModel"
	CanonReasoningEffort    = "reasoningEffort"
	CanonSystemPrompt       = "systemPrompt"       // full override
	CanonAppendSystemPrompt = "appendSystemPrompt" // appended to the agent's default
	CanonPermissionMode     = "permissionMode"
	CanonAllowedTools       = "allowedTools"      // restrict to these built-in tools
	CanonAllowRules         = "allowRules"        // permission allow rules
	CanonDenyRules          = "denyRules"         // permission deny rules
	CanonExtraDirs          = "extraDirs"         // additional accessible directories
	CanonMaxSpendUSD        = "maxSpendUsd"       // hard spend cap for the turn
	CanonMaxTurns           = "maxTurns"          // cap on agent turns (tool-use cycles) for the turn
	CanonAutoCompactTokens  = "autoCompactTokens" // auto-compact the conversation at this context-token threshold
)

// canonRegistry is the set of valid unified canonical keys. A unified field whose Canon is not in
// this set is a bug (fails ValidateSchema); it means an adapter invented a cross-agent concept the
// core doesn't know about.
var canonRegistry = map[string]bool{
	CanonModel:              true,
	CanonFallbackModel:      true,
	CanonReasoningEffort:    true,
	CanonSystemPrompt:       true,
	CanonAppendSystemPrompt: true,
	CanonPermissionMode:     true,
	CanonAllowedTools:       true,
	CanonAllowRules:         true,
	CanonDenyRules:          true,
	CanonExtraDirs:          true,
	CanonMaxSpendUSD:        true,
	CanonMaxTurns:           true,
	CanonAutoCompactTokens:  true,
}

// IsCanon reports whether key is a registered unified canonical key.
func IsCanon(key string) bool { return canonRegistry[key] }

// ValidateSchema enforces the taxonomy invariants on an adapter's settings schema:
//   - every field declares a Scope (unified | custom);
//   - a unified field's Canon is non-empty AND registered (no invented cross-agent concepts);
//   - a custom field's Canon equals its Key (custom concepts don't share a namespace);
//   - Canon is unique within the schema (canon→key must resolve unambiguously).
//
// It is called by an adapter conformance test so a mis-annotated field fails CI, not a live turn.
func ValidateSchema(schema SettingsSchema) error {
	seen := map[string]string{} // canon -> first key that used it
	for _, sec := range schema.Sections {
		for _, f := range sec.Fields {
			switch f.Scope {
			case ScopeUnified:
				if f.Canon == "" {
					return fmt.Errorf("field %q is unified but has no canon", f.Key)
				}
				if !IsCanon(f.Canon) {
					return fmt.Errorf("field %q has unregistered unified canon %q", f.Key, f.Canon)
				}
			case ScopeCustom:
				if f.Canon != f.Key {
					return fmt.Errorf("custom field %q must set canon == key, got canon %q", f.Key, f.Canon)
				}
			default:
				return fmt.Errorf("field %q has no scope (want unified|custom)", f.Key)
			}
			if prev, dup := seen[f.Canon]; dup {
				return fmt.Errorf("duplicate canon %q on fields %q and %q", f.Canon, prev, f.Key)
			}
			seen[f.Canon] = f.Key
		}
	}
	return nil
}

// CanonToKey resolves a canonical key to the given schema's adapter-specific field key. The bool is
// false when no field in the schema declares that canon. This is how the runner turns a client's
// canon-addressed per-turn option into the key the adapter stores/flags.
func CanonToKey(schema SettingsSchema, canon string) (string, bool) {
	for _, sec := range schema.Sections {
		for _, f := range sec.Fields {
			if f.Canon == canon {
				return f.Key, true
			}
		}
	}
	return "", false
}

// ResolveSettingKey maps an incoming sticky-config key to the schema's declared, non-secret RAW
// key it should persist under. It accepts either a raw key directly (back-compat) OR a canonical
// key that resolves via CanonToKey to a declared non-secret field — so the sticky setConfig path
// accepts "reasoningEffort" exactly like the canon-addressed per-turn path, landing it on whichever
// raw field the selected agent uses. Raw wins over canon (checked first). Returns ("", false) when
// the key is neither a declared non-secret raw key nor a canon resolving to one; callers skip those,
// preserving the existing "silently ignore unrecognized keys" behavior. The non-secret allow-list
// (SettingsKeys) guards both branches, so a canon can never resolve onto a secret field.
func ResolveSettingKey(schema SettingsSchema, key string) (string, bool) {
	allow := SettingsKeys(schema)
	if allow[key] {
		return key, true
	}
	if raw, ok := CanonToKey(schema, key); ok && allow[raw] {
		return raw, true
	}
	return "", false
}
