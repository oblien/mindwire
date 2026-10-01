package mindwire

import (
	"context"

	"github.com/oblien/mindwire/daemon/internal/conversations"
)

type Conversation = conversations.Conversation
type ConversationQuery = conversations.BrowseQuery
type ConversationPage = conversations.Page
type ConversationOpenRequest = conversations.OpenRequest
type ConversationOpenResult = conversations.OpenResult

// Conversations lists native history across every folder, including folders
// not yet registered as projects. Browsing never changes workspace metadata.
func (w *Workspace) Conversations(ctx context.Context, query ConversationQuery) (ConversationPage, error) {
	page, err := w.c.core.conversations.Browse(ctx, query)
	return page, workspaceError("Workspace.Conversations", err)
}

// OpenConversation attaches a native reference to the normal chat protocol.
// The returned chat retains its original folder and native resume identity.
func (w *Workspace) OpenConversation(ctx context.Context, request ConversationOpenRequest) (ConversationOpenResult, error) {
	result, err := w.c.core.conversations.Open(ctx, request)
	return result, workspaceError("Workspace.OpenConversation", err)
}
