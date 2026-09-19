package agent

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInputClosed means input was not submitted to a live native turn. Callers
// may safely resume the conversation instead; other errors must not auto-retry.
var ErrInputClosed = errors.New("the native turn is no longer accepting input")

// Interaction is a structured, self-describing request the agent surfaces mid-turn for
// the client to render generically — and, when NeedsResponse, for the user to answer.
// Each adapter maps its agent's native signals onto these kinds; the client switches on
// Kind, so a new kind needs no client release to at least render its title/detail.
//
//	todos    — a checklist (Items)                  Claude: TodoWrite
//	approval — approve/reject a tool/edit (Options)
//	form     — one or more questions answered together (Questions)
//	choice   — pick one of Options
//	select   — pick many of Options
//	input    — free-text answer
//	plan     — a proposed plan to accept (Detail + Options)
type Interaction struct {
	ID            string     `json:"id,omitempty"`
	Kind          string     `json:"kind"`
	Title         string     `json:"title,omitempty"`
	Detail        string     `json:"detail,omitempty"`
	Items         []TodoItem `json:"items,omitempty"`     // kind=todos
	Options       []Action   `json:"options,omitempty"`   // kind=approval|choice|select|plan
	Questions     []Question `json:"questions,omitempty"` // one form, answered atomically
	Blocking      *bool      `json:"blocking,omitempty"`  // nil means a pending request blocks
	Feedback      string     `json:"feedback,omitempty"`  // approval text: "always" or "rejection"; empty = unsupported
	NeedsResponse bool       `json:"needsResponse,omitempty"`
	// Message questions are non-blocking harness messages, not native control requests.
	// They remain answerable after the originating turn ends. The daemon owns RunID and
	// Response, including persistence and delivery as steering or a resumed user turn.
	ResponseMode string               `json:"responseMode,omitempty"` // "message"; empty = native control
	RunID        string               `json:"runId,omitempty"`
	Response     *InteractionResponse `json:"response,omitempty"`
	Meta         map[string]any       `json:"meta,omitempty"`
}

func (it Interaction) IsMessageQuestion() bool {
	return it.ResponseMode == "message" && len(it.Questions) > 0
}

// AnswerText is used only for native async questions, whose reply transport is an
// ordinary user message. Native control forms retain their structured RPC response.
func (it Interaction) AnswerText(reply InteractionResponse) string {
	var lines []string
	for _, q := range it.Questions {
		a := reply.Answers[q.ID]
		var selected []string
		for _, id := range a.Options {
			for _, option := range q.Options {
				if option.ID == id {
					selected = append(selected, option.Label)
					break
				}
			}
		}
		answer := strings.Join(selected, ", ")
		if text := strings.TrimSpace(a.Text); text != "" {
			if answer != "" {
				answer += "\nFeedback: "
			}
			answer += text
		}
		lines = append(lines, q.Title+"\n"+answer)
	}
	return strings.Join(lines, "\n\n")
}

// Question retains the harness's stable id, descriptions and selection rules.
// Text may accompany selected options as feedback; AllowOther also permits text alone.
type Question struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Header      string   `json:"header,omitempty"`
	Options     []Action `json:"options,omitempty"`
	MultiSelect bool     `json:"multiSelect,omitempty"`
	AllowOther  bool     `json:"allowOther,omitempty"`
	IsSecret    bool     `json:"isSecret,omitempty"`
	Optional    bool     `json:"optional,omitempty"`
}

type QuestionAnswer struct {
	Options []string `json:"options,omitempty"`
	Text    string   `json:"text,omitempty"`
}

type InteractionResponse struct {
	InteractionID string                    `json:"interactionId"`
	Decision      string                    `json:"decision,omitempty"`
	Options       []string                  `json:"options,omitempty"`
	Text          string                    `json:"text,omitempty"`
	Answers       map[string]QuestionAnswer `json:"answers,omitempty"`
}

// ValidateResponse rejects incomplete forms and unknown decisions before any harness
// can interpret an empty or misspelled approval as permission to execute a tool.
func (it Interaction) ValidateResponse(reply InteractionResponse) (InteractionResponse, error) {
	if len(it.Questions) > 0 {
		if len(reply.Answers) == 0 && len(it.Questions) == 1 {
			options := reply.Options
			text := reply.Text
			if len(options) == 0 && reply.Decision != "" {
				options = []string{reply.Decision}
			}
			if len(options) == 0 {
				for _, option := range it.Questions[0].Options {
					if text == option.ID || text == option.Label {
						options, text = []string{option.ID}, ""
						break
					}
				}
			}
			reply.Answers = map[string]QuestionAnswer{it.Questions[0].ID: {Options: options, Text: text}}
		}
		known := map[string]bool{}
		for _, q := range it.Questions {
			known[q.ID] = true
			a := reply.Answers[q.ID]
			if err := q.validate(a); err != nil {
				return reply, err
			}
		}
		for id := range reply.Answers {
			if !known[id] {
				return reply, fmt.Errorf("unknown question %q", id)
			}
		}
		return reply, nil
	}
	if it.Kind == "approval" || it.Kind == "plan" {
		for _, option := range it.Options {
			if reply.Decision != "" && reply.Decision == option.ID {
				return reply, nil
			}
		}
		return reply, fmt.Errorf("choose one of the offered approval actions")
	}
	options := reply.Options
	if len(options) == 0 && reply.Decision != "" {
		options = []string{reply.Decision}
	}
	// Older clients sent the chosen label as text.
	if len(options) == 0 {
		for _, option := range it.Options {
			if reply.Text == option.ID || reply.Text == option.Label {
				options = []string{option.ID}
				break
			}
		}
	}
	q := Question{ID: it.ID, Options: it.Options, MultiSelect: it.Kind == "select", AllowOther: it.Meta["allowOther"] == true}
	if err := q.validate(QuestionAnswer{Options: options, Text: reply.Text}); err != nil {
		return reply, err
	}
	reply.Options = options
	return reply, nil
}

func (q Question) validate(a QuestionAnswer) error {
	if !q.Optional && len(a.Options) == 0 && strings.TrimSpace(a.Text) == "" {
		return fmt.Errorf("answer question %q", q.ID)
	}
	if len(a.Options) == 0 && strings.TrimSpace(a.Text) != "" && len(q.Options) > 0 && !q.AllowOther {
		return fmt.Errorf("select an option for question %q", q.ID)
	}
	if !q.MultiSelect && len(a.Options) > 1 {
		return fmt.Errorf("question %q accepts one selection", q.ID)
	}
	valid, seen := map[string]bool{}, map[string]bool{}
	for _, option := range q.Options {
		valid[option.ID] = true
	}
	for _, id := range a.Options {
		if !valid[id] || seen[id] {
			return fmt.Errorf("invalid option %q for question %q", id, q.ID)
		}
		seen[id] = true
	}
	return nil
}

// TodoItem is one entry of a todos interaction (mirrors Claude's TodoWrite shape).
type TodoItem struct {
	Content string `json:"content"`
	Status  string `json:"status"` // pending | in_progress | completed
}
