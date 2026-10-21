package mindwire

import "github.com/oblien/mindwire/packages/daemon/internal/registry"

type ScheduleSpec = registry.ScheduleSpec
type Schedule = registry.Schedule

// ChatSchedules use the ordinary turn runner and native history. Background
// execution is opt-in through Options.RunScheduledJobs, shared with app jobs.
func (w *Workspace) ChatSchedules(chatID string) ([]Schedule, error) {
	jobs, err := w.c.core.registry.ChatSchedules(chatID)
	return jobs, workspaceError("Workspace.ChatSchedules", err)
}

func (w *Workspace) SaveChatSchedule(chatID string, spec ScheduleSpec) (Schedule, error) {
	w.c.core.registryMu.Lock()
	defer w.c.core.registryMu.Unlock()
	job, err := w.c.core.apps.SaveChatSchedule(chatID, spec)
	return job, workspaceError("Workspace.SaveChatSchedule", err)
}

func (w *Workspace) DeleteChatSchedule(chatID, id string) error {
	return workspaceError("Workspace.DeleteChatSchedule", w.c.core.apps.DeleteChatSchedule(chatID, id))
}
