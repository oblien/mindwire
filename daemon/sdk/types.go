package mindwire

// types.go re-exports the daemon core's public vocabulary as type ALIASES so a consumer of this SDK
// works entirely against `mindwire.*` and never has to import a `daemon/internal/*` package (which the
// Go toolchain would forbid anyway). Because these are aliases — not fresh named types — a value the
// SDK returns IS the underlying core value: no conversion, and json.Marshal produces exactly the wire
// shape the HTTP daemon emits. The one place a conversion happens is Messages' recorded-history
// fallback (session.Message → agent.Message), noted there.

import (
	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/procmon"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/setup"
)

// Streaming events (the unified turn stream). See Run.Stream.
type (
	Event = agent.Event
	// EventType is the kind discriminator on an Event; see the Event* consts. Additive to the wire — a
	// consumer that doesn't recognize a value should ignore that event.
	EventType      = agent.EventType
	ToolEvent      = agent.ToolEvent
	ResultInfo     = agent.ResultInfo
	CompactionInfo = agent.CompactionInfo
	// ContinuationInfo delimits one iteration of a global-resolve run on the merged parent stream (see
	// Client.Resolve). Carried on an EventContinuation. Field additions to Event/ResultInfo/RunRecord
	// (Subtype, ParentID, Kind, StopReason, Iterations) and to Capabilities (Resolve) propagate through
	// the aliases automatically; only this new named type and the EventContinuation const are added here.
	ContinuationInfo = agent.ContinuationInfo
)

// Deep tool normalization: the structured `Action` carried on both a streaming ToolEvent and a
// transcript ToolPart. Only the sub-object matching Kind is populated. See agent/toolaction.go.
type (
	ToolKind     = agent.ToolKind
	ToolAction   = agent.ToolAction
	FileChange   = agent.FileChange
	ShellCommand = agent.ShellCommand
	SearchQuery  = agent.SearchQuery
	WebSearch    = agent.WebSearch
	MCPCall      = agent.MCPCall
)

// ToolKind values, the canonical agent-independent tool classification.
const (
	KindFileEdit  = agent.KindFileEdit
	KindFileRead  = agent.KindFileRead
	KindShell     = agent.KindShell
	KindSearch    = agent.KindSearch
	KindWebSearch = agent.KindWebSearch
	KindWebFetch  = agent.KindWebFetch
	KindMCP       = agent.KindMCP
	KindOther     = agent.KindOther
)

// EventType values, one per kind of stream item.
const (
	EventSession      = agent.EventSession
	EventText         = agent.EventText
	EventThinking     = agent.EventThinking
	EventToolUse      = agent.EventToolUse
	EventToolResult   = agent.EventToolResult
	EventResult       = agent.EventResult
	EventError        = agent.EventError
	EventStatus       = agent.EventStatus
	EventInteraction  = agent.EventInteraction
	EventCompaction   = agent.EventCompaction
	EventContinuation = agent.EventContinuation
)

// Mid-turn interactions the agent surfaces (a question, a plan to approve, todo progress).
type (
	Interaction = agent.Interaction
	TodoItem    = agent.TodoItem
	Action      = agent.Action
)

// Turn inputs.
type (
	TurnOptions = agent.TurnOptions
	Attachment  = agent.Attachment
	Inbound     = agent.Inbound
)

// Chat transcript shapes.
type (
	Message      = agent.Message
	Part         = agent.Part
	ToolPart     = agent.ToolPart
	HistoryQuery = agent.HistoryQuery
)

// Capabilities + the dynamic settings schema.
type (
	Capabilities   = agent.Capabilities
	Support        = agent.Support
	Protocol       = agent.Protocol
	OutputMode     = agent.OutputMode
	CatalogEntry   = agent.CatalogEntry
	SettingsSchema = agent.SettingsSchema
	Section        = agent.Section
	Field          = agent.Field
	Option         = agent.Option
	FieldType      = agent.FieldType
	Scope          = agent.Scope
)

// Support values (a capability's fidelity for hybrid features like history).
const (
	SupportNone     = agent.SupportNone
	SupportNative   = agent.SupportNative
	SupportEmulated = agent.SupportEmulated
)

// ModelInfo is one entry in the agent's model list (GET /models), carrying provider-aware metadata
// when the on-disk catalog can supply it. ModelCost is its per-million-token pricing. See Client.Models
// and agent/models.go.
type (
	ModelInfo = agent.ModelInfo
	ModelCost = agent.ModelCost
)

// Persistent prompt/memory surface: the agent's memory file (CLAUDE.md / AGENTS.md) and its saved
// prompt templates (slash-commands / saved prompts), each addressed by a canonical MemoryScope. See
// Client.Prompts and agent/memory.go, agent/prompts.go.
type (
	MemoryScope    = agent.MemoryScope
	MemoryDoc      = agent.MemoryDoc
	PromptTemplate = agent.PromptTemplate
	Subagent       = agent.Subagent
	SubagentMeta   = agent.SubagentMeta
)

// MCPServer is one persistent MCP-server definition (GET/PUT/DELETE /mcp). See Client.MCP and
// agent/mcp.go. Carries an env-var NAME for HTTP auth (bearerTokenEnvVar), never a secret value.
type MCPServer = agent.MCPServer

// CustomProvider is one registered custom OpenAI-compatible LLM provider (GET/PUT/DELETE /providers).
// See Client.Providers and agent/providers.go. The API key is never part of this shape — it is supplied
// write-only on Set and reported only as HasKey.
type CustomProvider = agent.CustomProvider

// MemoryScope values — the two persistent layers a memory file or prompt template can live at.
const (
	MemoryProject = agent.MemoryProject
	MemoryUser    = agent.MemoryUser
)

// Protocol values — how the daemon drives an agent for a turn.
const (
	ProtocolCLI        = agent.ProtocolCLI
	ProtocolHTTP       = agent.ProtocolHTTP
	ProtocolPersistent = agent.ProtocolPersistent
)

// OutputMode values — how an agent emits a turn's output.
const (
	OutputStructuredJSON = agent.OutputStructuredJSON
	OutputTerminal       = agent.OutputTerminal
)

// Auth, health checks, and notifications.
type (
	AuthMethod       = agent.AuthMethod
	AuthState        = agent.AuthState
	AuthStatus       = agent.AuthStatus
	Check            = agent.Check
	Condition        = agent.Condition
	ConditionUX      = agent.ConditionUX
	Notification     = agent.Notification
	NotificationSpec = agent.NotificationSpec
	// Notifier is the delivery interface an extra notification channel implements. Pass one via
	// Options.Notifier to fan turn notifications out to your own sink alongside the built-in channels.
	Notifier = notify.Notifier
	// Daemon-driven fan-out: named delivery targets + the rules that route notifications to them.
	NotifyChannelType = agent.NotifyChannelType
	NotifyChannel     = agent.NotifyChannel
	NotifyRuleScope   = agent.NotifyRuleScope
	NotifyRule        = agent.NotifyRule
	// Live per-turn resource sampling (Client.Processes). A ProcessFrame is one sampling tick; each
	// ProcessSample is one running turn's summed CPU%/RSS, labeled (agent, chatId, runId). See procmon.
	ProcessFrame  = procmon.Frame
	ProcessSample = procmon.Sample
)

// Notification channel types (delivery payload shape).
const (
	ChannelWebhook  = agent.ChannelWebhook
	ChannelSlack    = agent.ChannelSlack
	ChannelDiscord  = agent.ChannelDiscord
	ChannelTelegram = agent.ChannelTelegram
)

// Notification rule scopes.
const (
	ScopeGlobal  = agent.ScopeGlobal
	ScopeAgent   = agent.ScopeAgent
	ScopeSession = agent.ScopeSession
)

// Health-check statuses.
const (
	CheckOK   = agent.CheckOK
	CheckWarn = agent.CheckWarn
	CheckFail = agent.CheckFail
)

// Notification conditions.
const (
	Finished        = agent.Finished
	Errored         = agent.Errored
	WaitingApproval = agent.WaitingApproval
	WaitingFeedback = agent.WaitingFeedback
	WaitingInput    = agent.WaitingInput
)

// Durable records + setup/health reports.
type (
	// RunRecord is the durable, persisted turn record (id/chat/agent/status/…). A live Run handle
	// wraps one; Run.Value / Run.Refresh return it.
	RunRecord    = session.Run
	ChatSummary  = session.ChatSummary
	StepResult   = setup.StepResult
	SetupStatus  = setup.Status
	DoctorReport = orchestrator.DoctorReport
)

// Canonical per-turn setting keys — the agent-agnostic addresses a client uses in TurnOptions.Settings
// (the runner maps each to the selected agent's own field key). Re-exported so callers avoid magic
// strings.
const (
	CanonModel              = agent.CanonModel
	CanonFallbackModel      = agent.CanonFallbackModel
	CanonReasoningEffort    = agent.CanonReasoningEffort
	CanonSystemPrompt       = agent.CanonSystemPrompt
	CanonAppendSystemPrompt = agent.CanonAppendSystemPrompt
	CanonPermissionMode     = agent.CanonPermissionMode
	CanonAllowedTools       = agent.CanonAllowedTools
	CanonAllowRules         = agent.CanonAllowRules
	CanonDenyRules          = agent.CanonDenyRules
	CanonExtraDirs          = agent.CanonExtraDirs
	CanonMaxSpendUSD        = agent.CanonMaxSpendUSD
	CanonMaxTurns           = agent.CanonMaxTurns
)
