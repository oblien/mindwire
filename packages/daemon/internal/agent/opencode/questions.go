package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/oblien/mindwire/packages/daemon/internal/agent"
)

// V1 questions are separate from permissions and require labels, in question order.
// Store the native schema on the interaction, never take correlators from the client.
type ocQuestion struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	Questions []struct {
		Header   string `json:"header"`
		Question string `json:"question"`
		Multiple bool   `json:"multiple"`
		Custom   *bool  `json:"custom"`
		Options  []struct {
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"options"`
	} `json:"questions"`
}

func (q ocQuestion) interaction() *agent.Interaction {
	raw, _ := json.Marshal(q)
	it := &agent.Interaction{ID: q.ID, Kind: "form", Title: "OpenCode has a question", NeedsResponse: true, Meta: map[string]any{"opencodeQuestion": string(raw)}}
	for i, question := range q.Questions {
		field := agent.Question{ID: strconv.Itoa(i), Title: question.Question, MultiSelect: question.Multiple, AllowOther: question.Custom == nil || *question.Custom}
		for _, option := range question.Options {
			field.Options = append(field.Options, agent.Action{ID: option.Label, Label: option.Label, Description: option.Description})
		}
		it.Questions = append(it.Questions, field)
	}
	return it
}

func (s server) replyQuestion(ctx context.Context, client *http.Client, base string, msg agent.Inbound) error {
	var question ocQuestion
	if json.Unmarshal([]byte(msg.Meta["opencodeQuestion"]), &question) != nil || question.ID != msg.InteractionID {
		return fmt.Errorf("invalid OpenCode question identity")
	}
	path := base + "/question/" + url.PathEscape(question.ID)
	if agent.Denied(msg.Decision) {
		return s.doJSON(ctx, client, http.MethodPost, path+"/reject", []byte(`{}`), nil)
	}
	answers := make([][]string, len(question.Questions))
	for i := range question.Questions {
		a := msg.Answers[strconv.Itoa(i)]
		answers[i] = append([]string{}, a.Options...)
		if a.Text != "" {
			answers[i] = append(answers[i], a.Text)
		}
	}
	body, _ := json.Marshal(map[string]any{"answers": answers})
	return s.doJSON(ctx, client, http.MethodPost, path+"/reply", body, nil)
}
