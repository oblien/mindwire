package bots

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/oblien/mindwire/packages/daemon/internal/registry"
	"github.com/oblien/mindwire/packages/daemon/internal/session"
)

type chatRunKey struct{}

// A scheduled prompt resolves the same saved chat and profile as an owner send.
// It retains the native session, approvals, instructions and notification rules.
func (s *Service) ScheduledTurn(chatID string, start func(registry.Chat, registry.Agent, registry.Project) (session.Run, error)) (session.Run, error) {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	chat, profile, project, err := s.db.NativeChatContext(chatID, s.history)
	if err != nil {
		return session.Run{}, err
	}
	if chat == nil {
		return session.Run{}, registry.ErrNotFound
	}
	if chat.BotID != "" {
		bot, err := s.db.Bot(chat.BotID)
		if err != nil {
			return session.Run{}, err
		}
		if bot.Archived {
			return session.Run{}, registry.ErrDeleted
		}
	}
	if err = s.db.CheckWorkPath(project.Path); err != nil {
		return session.Run{}, err
	}
	// This is an ordinary scheduled turn, not a manager delegating a new task.
	return start(*chat, *profile, *project)
}

func (s *Service) scheduleChat(actor registry.Bot, chatID string) error {
	chat, _, _, err := s.db.ChatContext(chatID)
	if err != nil {
		return err
	}
	if chat == nil {
		return registry.ErrNotFound
	}
	if actor.ID == "" {
		return nil
	} // authenticated owner only
	current, err := s.authorize(actor, actor.ID, false)
	if err != nil {
		return err
	}
	if !current.IsMaster && chat.BotID != current.ID {
		return fmt.Errorf("%w: this bot can only schedule its own conversations", registry.ErrInvalid)
	}
	return nil
}

func scheduleChatID(ctx context.Context, explicit string) string {
	if explicit != "" {
		return explicit
	}
	id, _ := ctx.Value(chatRunKey{}).(string)
	return id
}

func (s *Service) addScheduleTools(server *mcp.Server, actor registry.Bot) {
	if s.apps == nil {
		return
	}
	mcp.AddTool(server, &mcp.Tool{Name: "schedules_list", Description: "List this conversation's saved automatic prompts, next run, last run and errors. Omit chatId for this conversation. Use these for recurring AI work such as daily research reports, not fixed reminders. Managers may choose another saved chat; remote operations need workspaceId and explicit chatId."}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		WorkspaceID string `json:"workspaceId,omitempty"`
		ChatID      string `json:"chatId,omitempty"`
	}) (*mcp.CallToolResult, any, error) {
		if out, routed, err := s.routeTool(ctx, actor, "schedules_list", in); routed {
			return nil, out, err
		}
		id := scheduleChatID(ctx, in.ChatID)
		if err := s.scheduleChat(actor, id); err != nil {
			return nil, nil, err
		}
		jobs, err := s.db.ChatSchedules(id)
		return nil, map[string]any{"schedules": jobs}, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "schedules_save", Description: "Save an automatic agent prompt in this same conversation. Example: every morning research X and generate a report here. Omit chatId for this conversation. Supply name, prompt, and localTime HH:mm with an IANA timeZone (optionally weekdays 0=Sunday), intervalSeconds>=60, or one-time runAt with offset. Ask briefly if morning time or timezone is unknown; never guess a timezone. The prompt is the work to perform at each occurrence, not an instruction to create another schedule. Each run uses the saved chat's native context and normal approvals and sends its ordinary completion notification if configured and unmuted. The owning computer must be awake with Mindwire running; the phone can be closed. A busy chat waits. Read schedules_list first to avoid duplicates. Update with id and expectedRevision; enabled=false pauses. No scripts, cron or permanently running AI turns. Never claim success before this tool succeeds."}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		WorkspaceID string `json:"workspaceId,omitempty"`
		ChatID      string `json:"chatId,omitempty"`
		registry.ScheduleSpec
	}) (*mcp.CallToolResult, any, error) {
		if out, routed, err := s.routeTool(ctx, actor, "schedules_save", in); routed {
			return nil, out, err
		}
		id := scheduleChatID(ctx, in.ChatID)
		if err := s.scheduleChat(actor, id); err != nil {
			return nil, nil, err
		}
		job, err := s.apps.SaveChatSchedule(id, in.ScheduleSpec)
		return nil, map[string]any{"schedule": job}, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "schedules_delete", Description: "Delete an automatic prompt from this conversation when the user asks. Omit chatId for this conversation. Does not delete messages or stop a turn already running."}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		WorkspaceID string `json:"workspaceId,omitempty"`
		ChatID      string `json:"chatId,omitempty"`
		ScheduleID  string `json:"scheduleId"`
	}) (*mcp.CallToolResult, any, error) {
		if out, routed, err := s.routeTool(ctx, actor, "schedules_delete", in); routed {
			return nil, out, err
		}
		id := scheduleChatID(ctx, in.ChatID)
		if err := s.scheduleChat(actor, id); err != nil {
			return nil, nil, err
		}
		err := s.apps.DeleteChatSchedule(id, in.ScheduleID)
		return nil, map[string]bool{"removed": err == nil}, err
	})
}
