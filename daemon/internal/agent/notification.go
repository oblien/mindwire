package agent

// Notifications are part of the unified agent protocol. An agent declares which
// conditions it produces and their UX (NotificationSpec, the "inline notification
// adapter"); the daemon emits a Notification and hands it to a Notifier (→ the configured webhook).

// Condition is a unified agent state worth notifying about.
type Condition string

const (
	Finished        Condition = "finished"         // turn completed
	Errored         Condition = "error"            // turn failed
	WaitingApproval Condition = "waiting_approval" // needs the user to approve a tool/edit to continue
	WaitingFeedback Condition = "waiting_feedback" // needs the user's answer to continue
	WaitingInput    Condition = "waiting_input"    // generic awaiting input
)

// Action is a user action surfaced with the notification (e.g. Approve / Reject).
type Action struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Notification is the unified payload the daemon emits; the webhook receiver fans it out.
type Notification struct {
	Condition Condition `json:"condition"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Agent     string    `json:"agent,omitempty"`
	ChatID    string    `json:"chatId,omitempty"`
	RunID     string    `json:"runId,omitempty"`
	Actions   []Action  `json:"actions,omitempty"`
}

// NotifyChannelType is the delivery shape of a notification channel. It selects only how the
// outgoing payload is framed — every type is ultimately an HTTP POST to a URL the client owns.
type NotifyChannelType string

const (
	ChannelWebhook  NotifyChannelType = "webhook"  // raw Notification JSON (optionally HMAC-signed)
	ChannelSlack    NotifyChannelType = "slack"    // {"text": "<title>\n<body>"} — Slack incoming webhook
	ChannelDiscord  NotifyChannelType = "discord"  // {"content": "<title>\n<body>"} — Discord webhook
	ChannelTelegram NotifyChannelType = "telegram" // {"text": "<title>\n<body>"} — bot sendMessage URL
)

// NotifyChannel is one named delivery target: a webhook URL plus optional auth (custom headers, a
// bearer token, and — for the webhook type — an HMAC signing secret). The daemon owns the list and
// persists it; a rule references channels by ID. Token/Secret/Headers are secrets: the API masks
// them on read and merge-preserves them on write.
type NotifyChannel struct {
	ID      string            `json:"id"`
	Type    NotifyChannelType `json:"type"`
	Label   string            `json:"label,omitempty"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"` // extra request headers (values are secret)
	Token   string            `json:"token,omitempty"`   // sent as Authorization: Bearer <token>
	Secret  string            `json:"secret,omitempty"`  // webhook type only: HMAC-SHA256 signing key
	Enabled bool              `json:"enabled"`
}

// NotifyRuleScope selects which notifications a rule applies to by origin.
type NotifyRuleScope string

const (
	ScopeGlobal  NotifyRuleScope = "global"  // every notification
	ScopeAgent   NotifyRuleScope = "agent"   // only notifications from a given agent (adapter type)
	ScopeSession NotifyRuleScope = "session" // only notifications from a given chat/session (ChatID)
)

// NotifyRule routes matching notifications to a set of channels. A rule fires when it is enabled, the
// notification's condition is in Conditions (empty = all conditions), and its scope matches: global
// always; agent when Agent == n.Agent; session when Session == n.ChatID.
type NotifyRule struct {
	ID         string          `json:"id"`
	Scope      NotifyRuleScope `json:"scope"`
	Agent      string          `json:"agent,omitempty"`   // scope=agent: the adapter type to match
	Session    string          `json:"session,omitempty"` // scope=session: the chatId to match
	Conditions []Condition     `json:"conditions,omitempty"`
	ChannelIDs []string        `json:"channelIds"`
	Enabled    bool            `json:"enabled"`
}

// Matches reports whether this rule should route the given notification.
func (r NotifyRule) Matches(n Notification) bool {
	if !r.Enabled {
		return false
	}
	if len(r.Conditions) > 0 {
		hit := false
		for _, c := range r.Conditions {
			if c == n.Condition {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	switch r.Scope {
	case ScopeGlobal:
		return true
	case ScopeAgent:
		return r.Agent != "" && r.Agent == n.Agent
	case ScopeSession:
		return r.Session != "" && r.Session == n.ChatID
	default:
		return false
	}
}

// ConditionUX is an agent's per-condition notification UX.
type ConditionUX struct {
	Condition Condition `json:"condition"`
	Title     string    `json:"title"`
	Actions   []Action  `json:"actions,omitempty"`
}

// NotificationSpec is the set of conditions an agent declares + their UX.
type NotificationSpec struct {
	Conditions []ConditionUX `json:"conditions"`
}

// For returns the declared UX for a condition, if any.
func (s NotificationSpec) For(c Condition) (ConditionUX, bool) {
	for _, u := range s.Conditions {
		if u.Condition == c {
			return u, true
		}
	}
	return ConditionUX{}, false
}

// DefaultTitle is the fallback title for a condition the agent didn't customize.
func DefaultTitle(c Condition, agentName string) string {
	if agentName == "" {
		agentName = "Agent"
	}
	switch c {
	case Finished:
		return agentName + " finished"
	case Errored:
		return agentName + " hit an error"
	case WaitingApproval:
		return agentName + " needs your approval"
	case WaitingFeedback:
		return agentName + " is waiting for your reply"
	case WaitingInput:
		return agentName + " needs your input"
	default:
		return agentName
	}
}
