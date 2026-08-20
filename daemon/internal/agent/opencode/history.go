package opencode

import "github.com/oblien/mindwire/daemon/internal/agent"

// History is emulated for opencode (Capabilities.History == SupportEmulated): opencode persists
// sessions in its own SQLite store (opencode.db) whose schema is undocumented and not a stable public
// surface, so the adapter reads nothing from it and the core serves mindwire's own recorded stream
// instead. Returning (nil, nil) — not an error — is the contract for an emulated-history adapter.
// Consequently no HistoryDeleter is implemented (there is no adapter-owned transcript file to delete).
func (adapter) History(_ agent.HistoryQuery) ([]agent.Message, error) { return nil, nil }
