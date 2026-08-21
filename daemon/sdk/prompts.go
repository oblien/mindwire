package mindwire

import (
	"errors"
	"io/fs"
	"net/http"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Prompts is the persistent prompt/memory sub-API, reachable as Client.Prompts. It exposes the three
// layers beneath a turn's per-turn Options.SystemPrompt: an agent's memory file (Claude's CLAUDE.md,
// Codex's AGENTS.md) and its saved prompt templates (Claude slash-commands, Codex saved prompts). Each
// call maps one-for-one to a /memory or /prompts HTTP route and enforces the same gates: an agent
// without the underlying optional module surfaces as APIError{400}; an unsupported scope is 400; a
// missing template is APIError{404}. It is scoped to its client's default agent and rebinds under
// WithAgent; override per call with ForAgent.
//
// dir selects the project-scope working directory — empty resolves to the client's cwd (else the
// process cwd), matching how turns pick a working directory. None of these files is a secret.
type Prompts struct{ c *Client }

// memoryModule resolves the scoped agent and type-asserts its optional MemoryModule, returning
// APIError{400} when the agent doesn't expose one (mirroring the /memory handler's capability gate).
func (p *Prompts) memoryModule(op string, opts []ScopedOption) (agent.MemoryModule, error) {
	ag, err := p.c.resolve(opts)
	if err != nil {
		return nil, err
	}
	mod, ok := ag.Adapter.(agent.MemoryModule)
	if !ok {
		return nil, &APIError{Message: "agent does not support memory files", Status: http.StatusBadRequest, Op: op}
	}
	return mod, nil
}

// promptsModule resolves the scoped agent and type-asserts its optional PromptsModule, returning
// APIError{400} when the agent doesn't expose one (mirroring the /prompts handler's capability gate).
func (p *Prompts) promptsModule(op string, opts []ScopedOption) (agent.PromptsModule, error) {
	ag, err := p.c.resolve(opts)
	if err != nil {
		return nil, err
	}
	mod, ok := ag.Adapter.(agent.PromptsModule)
	if !ok {
		return nil, &APIError{Message: "agent does not support prompt templates", Status: http.StatusBadRequest, Op: op}
	}
	return mod, nil
}

// subagentsModule resolves the scoped agent and type-asserts its optional SubagentsModule, returning
// APIError{400} when the agent doesn't expose one (mirroring the /subagents handler's capability gate).
func (p *Prompts) subagentsModule(op string, opts []ScopedOption) (agent.SubagentsModule, error) {
	ag, err := p.c.resolve(opts)
	if err != nil {
		return nil, err
	}
	mod, ok := ag.Adapter.(agent.SubagentsModule)
	if !ok {
		return nil, &APIError{Message: "agent does not support subagent definitions", Status: http.StatusBadRequest, Op: op}
	}
	return mod, nil
}

// dir resolves the project directory a project-scope op targets: an explicit dir wins, else the
// client's cwd (else the process cwd). Mirrors the HTTP dirParam helper.
func (p *Prompts) dir(dir string) string {
	return agent.ResolveDir(dir, p.c.core.sup.CWD())
}

// Memory returns the scoped agent's memory file at every supported scope (project + user for both
// Claude and Codex). Each doc carries its resolved path and whether the file exists; an absent file
// has Exists=false and empty Content. A read error surfaces as APIError{500}, matching GET /memory.
func (p *Prompts) Memory(dir string, opts ...ScopedOption) ([]MemoryDoc, error) {
	mod, err := p.memoryModule("Prompts.Memory", opts)
	if err != nil {
		return nil, err
	}
	resolved := p.dir(dir)
	out := []MemoryDoc{}
	for _, scope := range mod.MemoryScopes() {
		doc, rerr := mod.ReadMemory(scope, resolved)
		if rerr != nil {
			return nil, &APIError{Message: rerr.Error(), Status: http.StatusInternalServerError, Op: "Prompts.Memory", Cause: rerr}
		}
		out = append(out, doc)
	}
	return out, nil
}

// SetMemory writes the scoped agent's memory file at scope and returns the resulting doc (resolved
// path, Exists=true). A bad scope or a missing project directory is a caller error → APIError{400},
// matching PUT /memory.
func (p *Prompts) SetMemory(scope MemoryScope, dir, content string, opts ...ScopedOption) (MemoryDoc, error) {
	mod, err := p.memoryModule("Prompts.SetMemory", opts)
	if err != nil {
		return MemoryDoc{}, err
	}
	doc, werr := mod.WriteMemory(scope, p.dir(dir), content)
	if werr != nil {
		return MemoryDoc{}, &APIError{Message: werr.Error(), Status: http.StatusBadRequest, Op: "Prompts.SetMemory", Cause: werr}
	}
	return doc, nil
}

// DeleteMemory removes the scoped agent's memory file at scope and returns the resulting doc
// (Exists=false). An empty scope defaults to user. Idempotent — deleting an absent file still
// succeeds. A bad scope is APIError{400}, matching DELETE /memory.
func (p *Prompts) DeleteMemory(scope MemoryScope, dir string, opts ...ScopedOption) (MemoryDoc, error) {
	mod, err := p.memoryModule("Prompts.DeleteMemory", opts)
	if err != nil {
		return MemoryDoc{}, err
	}
	doc, derr := mod.DeleteMemory(orScope(scope), p.dir(dir))
	if derr != nil {
		return MemoryDoc{}, &APIError{Message: derr.Error(), Status: http.StatusBadRequest, Op: "Prompts.DeleteMemory", Cause: derr}
	}
	return doc, nil
}

// List returns the scoped agent's saved prompt templates across every supported scope (Claude:
// project + user; Codex: user only). Content is omitted here — fetch it with Get. A missing prompt
// directory yields an empty list for that scope, not an error; any other failure is APIError{500},
// matching GET /prompts.
func (p *Prompts) List(dir string, opts ...ScopedOption) ([]PromptTemplate, error) {
	mod, err := p.promptsModule("Prompts.List", opts)
	if err != nil {
		return nil, err
	}
	tpls, lerr := mod.ListPrompts(p.dir(dir))
	if lerr != nil {
		return nil, &APIError{Message: lerr.Error(), Status: http.StatusInternalServerError, Op: "Prompts.List", Cause: lerr}
	}
	return tpls, nil
}

// Get returns one template's full content. An empty scope defaults to user — the scope every
// prompt-supporting agent has. A missing template maps to APIError{404}; an invalid name or
// unsupported scope to APIError{400}, matching GET /prompts/{name}.
func (p *Prompts) Get(scope MemoryScope, dir, name string, opts ...ScopedOption) (PromptTemplate, error) {
	mod, err := p.promptsModule("Prompts.Get", opts)
	if err != nil {
		return PromptTemplate{}, err
	}
	tpl, rerr := mod.ReadPrompt(orScope(scope), p.dir(dir), name)
	switch {
	case errors.Is(rerr, fs.ErrNotExist):
		return PromptTemplate{}, &APIError{Message: "prompt not found", Status: http.StatusNotFound, Op: "Prompts.Get", Cause: rerr}
	case rerr != nil:
		return PromptTemplate{}, &APIError{Message: rerr.Error(), Status: http.StatusBadRequest, Op: "Prompts.Get", Cause: rerr}
	}
	return tpl, nil
}

// Set creates or overwrites a template and returns it. An empty scope defaults to user. An invalid
// name or unsupported scope is APIError{400}, matching PUT /prompts/{name}.
func (p *Prompts) Set(scope MemoryScope, dir, name, content string, opts ...ScopedOption) (PromptTemplate, error) {
	mod, err := p.promptsModule("Prompts.Set", opts)
	if err != nil {
		return PromptTemplate{}, err
	}
	tpl, werr := mod.WritePrompt(orScope(scope), p.dir(dir), name, content)
	if werr != nil {
		return PromptTemplate{}, &APIError{Message: werr.Error(), Status: http.StatusBadRequest, Op: "Prompts.Set", Cause: werr}
	}
	return tpl, nil
}

// Delete removes one template at scope. An empty scope defaults to user. Idempotent — deleting an
// absent template still succeeds. An invalid name or unsupported scope is APIError{400}, matching
// DELETE /prompts/{name}.
func (p *Prompts) Delete(scope MemoryScope, dir, name string, opts ...ScopedOption) error {
	mod, err := p.promptsModule("Prompts.Delete", opts)
	if err != nil {
		return err
	}
	if derr := mod.DeletePrompt(orScope(scope), p.dir(dir), name); derr != nil {
		return &APIError{Message: derr.Error(), Status: http.StatusBadRequest, Op: "Prompts.Delete", Cause: derr}
	}
	return nil
}

// Subagents returns the scoped agent's persistent subagent definitions across every supported scope
// (Claude: project + user). Raw Content is omitted here (fetch it with Subagent); parsed Meta is kept.
// A missing definitions directory yields an empty list, not an error; any other failure is
// APIError{500}, matching GET /subagents. An agent without the module is APIError{400}. Distinct from a
// turn's per-turn Options.Subagents passthrough — this is the on-disk definition store.
func (p *Prompts) Subagents(dir string, opts ...ScopedOption) ([]Subagent, error) {
	mod, err := p.subagentsModule("Prompts.Subagents", opts)
	if err != nil {
		return nil, err
	}
	subs, lerr := mod.ListSubagents(p.dir(dir))
	if lerr != nil {
		return nil, &APIError{Message: lerr.Error(), Status: http.StatusInternalServerError, Op: "Prompts.Subagents", Cause: lerr}
	}
	return subs, nil
}

// Subagent returns one definition's full raw Content plus parsed Meta. An empty scope defaults to user.
// A missing definition maps to APIError{404}; an invalid name or unsupported scope to APIError{400},
// matching GET /subagents/{name}.
func (p *Prompts) Subagent(scope MemoryScope, dir, name string, opts ...ScopedOption) (Subagent, error) {
	mod, err := p.subagentsModule("Prompts.Subagent", opts)
	if err != nil {
		return Subagent{}, err
	}
	sub, rerr := mod.ReadSubagent(orScope(scope), p.dir(dir), name)
	switch {
	case errors.Is(rerr, fs.ErrNotExist):
		return Subagent{}, &APIError{Message: "subagent not found", Status: http.StatusNotFound, Op: "Prompts.Subagent", Cause: rerr}
	case rerr != nil:
		return Subagent{}, &APIError{Message: rerr.Error(), Status: http.StatusBadRequest, Op: "Prompts.Subagent", Cause: rerr}
	}
	return sub, nil
}

// SetSubagent creates or overwrites a definition verbatim (raw content is canonical) and returns it
// with parsed Meta. An empty scope defaults to user. An invalid name or unsupported scope is
// APIError{400}, matching PUT /subagents/{name}.
func (p *Prompts) SetSubagent(scope MemoryScope, dir, name, content string, opts ...ScopedOption) (Subagent, error) {
	mod, err := p.subagentsModule("Prompts.SetSubagent", opts)
	if err != nil {
		return Subagent{}, err
	}
	sub, werr := mod.WriteSubagent(orScope(scope), p.dir(dir), name, content)
	if werr != nil {
		return Subagent{}, &APIError{Message: werr.Error(), Status: http.StatusBadRequest, Op: "Prompts.SetSubagent", Cause: werr}
	}
	return sub, nil
}

// DeleteSubagent removes one definition at scope. An empty scope defaults to user. Idempotent —
// deleting an absent definition still succeeds. An invalid name or unsupported scope is APIError{400},
// matching DELETE /subagents/{name}.
func (p *Prompts) DeleteSubagent(scope MemoryScope, dir, name string, opts ...ScopedOption) error {
	mod, err := p.subagentsModule("Prompts.DeleteSubagent", opts)
	if err != nil {
		return err
	}
	if derr := mod.DeleteSubagent(orScope(scope), p.dir(dir), name); derr != nil {
		return &APIError{Message: derr.Error(), Status: http.StatusBadRequest, Op: "Prompts.DeleteSubagent", Cause: derr}
	}
	return nil
}

// orScope defaults an empty scope to user, mirroring the HTTP promptScope helper.
func orScope(s MemoryScope) MemoryScope {
	if s == "" {
		return MemoryUser
	}
	return s
}
