package agent

// PromptTemplate is one saved, reusable prompt — a Claude custom slash-command
// (.claude/commands/<name>.md) or a Codex saved prompt (~/.codex/prompts/<name>.md). Name is the
// bare template name (no directory, no ".md" suffix); Content is omitted from LIST results and
// populated only on a single READ, so listing stays cheap.
type PromptTemplate struct {
	Name    string      `json:"name"`
	Scope   MemoryScope `json:"scope"`
	Path    string      `json:"path"`
	Content string      `json:"content,omitempty"`
}

// PromptsModule is an OPTIONAL adapter capability (type-asserted like Titler): list, read, and write
// the agent's saved prompt templates. An adapter that implements it sets
// Capabilities.PromptTemplates=true; the API type-asserts this interface as the authoritative gate
// before serving /prompts.
type PromptsModule interface {
	// PromptScopes lists the scopes this agent's templates support. Claude supports both project and
	// user; Codex supports user only (a project-scope op → unsupported-scope error).
	PromptScopes() []MemoryScope
	// ListPrompts returns every template across the supported scopes with Content omitted. dir is the
	// resolved project directory. A missing directory yields NO entries for that scope (reads are
	// forgiving; only writes fail on a missing directory).
	ListPrompts(dir string) ([]PromptTemplate, error)
	// ReadPrompt returns one template's full content. A missing template returns an error satisfying
	// errors.Is(err, fs.ErrNotExist), which the API maps to 404.
	ReadPrompt(scope MemoryScope, dir, name string) (PromptTemplate, error)
	// WritePrompt writes a template (creating the prompt directory as needed) and returns it.
	WritePrompt(scope MemoryScope, dir, name, content string) (PromptTemplate, error)
	// DeletePrompt removes one template. Removing an absent template (or from a missing directory) is
	// not an error (idempotent) — the post-state is "no such template" either way.
	DeletePrompt(scope MemoryScope, dir, name string) error
}
