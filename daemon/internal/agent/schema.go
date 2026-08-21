package agent

// Settings schema is owned by this Go binary and served to the app, which
// renders it dynamically — the app hardcodes nothing about any agent. Adding a
// field or an agent means shipping a new daemon, not an app release.

type FieldType string

const (
	FieldText   FieldType = "text"
	FieldSecret FieldType = "secret"      // masked input; persisted in the sandbox state via the auth flow, never returned to the client
	FieldSelect FieldType = "select"      // one-of, from Options
	FieldMulti  FieldType = "multiselect" // many-of, from Options (stored comma-joined)
	FieldToggle FieldType = "toggle"
)

type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Scope marks whether a field is a UNIFIED cross-agent concept — one every agent has some
// form of (model, permission mode, system prompt) — or a CUSTOM agent-specific one. Unified
// fields carry a stable Canon key (see canon.go) the client addresses regardless of which
// agent is selected; the adapter maps that canon to its own key/flag. Custom fields are still
// typed & declared (shipped in a daemon build, never open passthrough) and conventionally set
// Canon == Key. This split is the normalization contract: the unified surface is a closed,
// declared vocabulary, and everything agent-specific is explicitly marked as such.
type Scope string

const (
	ScopeUnified Scope = "unified"
	ScopeCustom  Scope = "custom"
)

type Field struct {
	Key         string    `json:"key"`
	Label       string    `json:"label"`
	Type        FieldType `json:"type"`
	Scope       Scope     `json:"scope,omitempty"` // unified | custom (taxonomy)
	Canon       string    `json:"canon,omitempty"` // stable cross-agent key; == Key for custom fields
	Required    bool      `json:"required,omitempty"`
	Placeholder string    `json:"placeholder,omitempty"`
	Help        string    `json:"help,omitempty"`
	Options     []Option  `json:"options,omitempty"` // for select
	Default     string    `json:"default,omitempty"`
}

// Section groups fields under a heading (the app renders one group per section).
type Section struct {
	Title  string  `json:"title"`
	Fields []Field `json:"fields"`
}

type SettingsSchema struct {
	Sections []Section `json:"sections"`
}

// CatalogEntry is the minimum the create-screen picker needs (no schema).
type CatalogEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Tagline string `json:"tagline"`
}

// Step is one ensure-or-install in an agent's toolchain. Check runs first
// (exit 0 = already satisfied); otherwise Install runs. Requires names other
// tools (catalog or sibling steps) that must be satisfied BEFORE this one, so the
// resolver can order dependencies-first and dedup shared tools across agents.
// Shell commands aren't serialized to the app — install is the daemon's job.
type Step struct {
	Name     string   `json:"name"`
	Check    string   `json:"-"`
	Install  string   `json:"-"`
	Requires []string `json:"-"`
}

// SettingsKeys is the allow-list of NON-secret setting keys an agent declares. It is the
// exact set of keys /config may read or write AND the set the runner passes into a turn's
// TurnInput.Config. Secrets live outside the settings schema (touched only by the auth
// flow), so config can neither leak nor overwrite them, and they never enter a turn's
// Config — only its Env (via AuthModule.EnvForRun).
func SettingsKeys(schema SettingsSchema) map[string]bool {
	keys := map[string]bool{}
	for _, sec := range schema.Sections {
		for _, f := range sec.Fields {
			if f.Type != FieldSecret {
				keys[f.Key] = true
			}
		}
	}
	return keys
}

// Configured reports whether every required text/secret field has a value.
func Configured(schema SettingsSchema, config map[string]string) bool {
	for _, sec := range schema.Sections {
		for _, f := range sec.Fields {
			if !f.Required {
				continue
			}
			if f.Type == FieldSelect || f.Type == FieldMulti || f.Type == FieldToggle {
				continue
			}
			if config[f.Key] == "" {
				return false
			}
		}
	}
	return true
}
