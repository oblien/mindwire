package agent

import "testing"

func TestValidateSchemaAcceptsWellFormed(t *testing.T) {
	schema := SettingsSchema{Sections: []Section{{
		Title: "General",
		Fields: []Field{
			{Key: "model", Label: "Model", Type: FieldText, Scope: ScopeUnified, Canon: CanonModel},
			{Key: "flavor", Label: "Flavor", Type: FieldText, Scope: ScopeCustom, Canon: "flavor"},
		},
	}}}
	if err := ValidateSchema(schema); err != nil {
		t.Fatalf("well-formed schema rejected: %v", err)
	}
}

func TestValidateSchemaRejects(t *testing.T) {
	cases := map[string]SettingsSchema{
		"missing scope": {Sections: []Section{{Fields: []Field{
			{Key: "x", Canon: "x"},
		}}}},
		"unified without canon": {Sections: []Section{{Fields: []Field{
			{Key: "x", Scope: ScopeUnified},
		}}}},
		"unregistered unified canon": {Sections: []Section{{Fields: []Field{
			{Key: "x", Scope: ScopeUnified, Canon: "notARealCanon"},
		}}}},
		"custom canon != key": {Sections: []Section{{Fields: []Field{
			{Key: "x", Scope: ScopeCustom, Canon: "y"},
		}}}},
		"duplicate canon": {Sections: []Section{{Fields: []Field{
			{Key: "a", Scope: ScopeUnified, Canon: CanonModel},
			{Key: "b", Scope: ScopeUnified, Canon: CanonModel},
		}}}},
	}
	for name, schema := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateSchema(schema); err == nil {
				t.Fatalf("expected %s to be rejected, got nil", name)
			}
		})
	}
}

func TestCanonToKey(t *testing.T) {
	schema := SettingsSchema{Sections: []Section{{Fields: []Field{
		{Key: "effort", Scope: ScopeUnified, Canon: CanonReasoningEffort},
	}}}}
	if key, ok := CanonToKey(schema, CanonReasoningEffort); !ok || key != "effort" {
		t.Fatalf("CanonToKey(reasoningEffort) = %q,%v; want effort,true", key, ok)
	}
	if _, ok := CanonToKey(schema, CanonModel); ok {
		t.Fatalf("CanonToKey(model) resolved unexpectedly")
	}
}

// TestResolveSettingKey covers the sticky-config resolver: a raw key passes through (back-compat),
// a canon resolves to its raw key, an unknown key is dropped, and a canon can never land on a
// secret field (the non-secret allow-list guards both branches).
func TestResolveSettingKey(t *testing.T) {
	schema := SettingsSchema{Sections: []Section{{Fields: []Field{
		{Key: "effort", Scope: ScopeUnified, Canon: CanonReasoningEffort, Type: FieldSelect},
		{Key: "apiKey", Scope: ScopeCustom, Canon: "apiKey", Type: FieldSecret},
	}}}}

	// raw key wins (back-compat): a declared non-secret raw key passes straight through.
	if raw, ok := ResolveSettingKey(schema, "effort"); !ok || raw != "effort" {
		t.Fatalf("ResolveSettingKey(effort) = %q,%v; want effort,true", raw, ok)
	}
	// canon resolves to the agent's raw key.
	if raw, ok := ResolveSettingKey(schema, CanonReasoningEffort); !ok || raw != "effort" {
		t.Fatalf("ResolveSettingKey(reasoningEffort) = %q,%v; want effort,true", raw, ok)
	}
	// an unknown key (neither raw nor a resolvable canon) is dropped.
	if _, ok := ResolveSettingKey(schema, "bogusKey"); ok {
		t.Fatalf("ResolveSettingKey(bogusKey) resolved unexpectedly")
	}
	// a secret field is never writable through this path (not in the non-secret allow-list),
	// even by its own raw key.
	if _, ok := ResolveSettingKey(schema, "apiKey"); ok {
		t.Fatalf("ResolveSettingKey(apiKey) resolved a secret field")
	}
}
