package agent

// Interaction is a structured, self-describing request the agent surfaces mid-turn for
// the client to render generically — and, when NeedsResponse, for the user to answer.
// Each adapter maps its agent's native signals onto these kinds; the client switches on
// Kind, so a new kind needs no client release to at least render its title/detail.
//
//	todos    — a checklist (Items)                  Claude: TodoWrite
//	approval — approve/reject a tool/edit (Options)  Claude: permission request (later)
//	choice   — pick one of Options
//	select   — pick many of Options
//	input    — free-text answer
//	plan     — a proposed plan to accept (Detail + Options)
type Interaction struct {
	ID            string         `json:"id,omitempty"`
	Kind          string         `json:"kind"`
	Title         string         `json:"title,omitempty"`
	Detail        string         `json:"detail,omitempty"`
	Items         []TodoItem     `json:"items,omitempty"`   // kind=todos
	Options       []Action       `json:"options,omitempty"` // kind=approval|choice|select|plan
	NeedsResponse bool           `json:"needsResponse,omitempty"`
	Meta          map[string]any `json:"meta,omitempty"`
}

// TodoItem is one entry of a todos interaction (mirrors Claude's TodoWrite shape).
type TodoItem struct {
	Content string `json:"content"`
	Status  string `json:"status"` // pending | in_progress | completed
}
