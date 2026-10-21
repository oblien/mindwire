package shortcuts

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/oblien/mindwire/packages/daemon/internal/registry"
	"github.com/oblien/mindwire/packages/daemon/internal/session"
	"github.com/oblien/mindwire/packages/daemon/internal/workspaceexec"
)

// Chat jobs use the host's normal turn runner and transcript, never a shell CLI
// invocation, another worker process, or another notification pipeline.
type ChatScheduleRuntime interface {
	StartScheduledTurn(chatID, prompt, requestID string) (session.Run, error)
	ScheduledRun(runID string) (session.Run, bool)
}

func (s *Service) SetChatRuntime(runtime ChatScheduleRuntime) { s.chatRuntime = runtime }

func (s *Service) SaveChatSchedule(chatID string, spec registry.ScheduleSpec) (registry.Schedule, error) {
	if s.chatRuntime == nil {
		return registry.Schedule{}, errors.New("scheduled conversations are unavailable")
	}
	job, err := s.db.SaveChatSchedule(chatID, spec)
	if err == nil {
		s.Wake()
	}
	return job, err
}

func (s *Service) DeleteChatSchedule(chatID, id string) error {
	if err := s.db.DeleteChatSchedule(chatID, id); err != nil {
		return err
	}
	s.Wake()
	return nil
}

// StartScheduler opts this runtime into owning persisted background jobs. Merely
// constructing a service or connecting its notification store executes no work.
func (s *Service) StartScheduler() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return context.Canceled
	}
	if s.schedulerStarted {
		return nil
	}
	s.schedulerStarted = true
	s.background.Add(1)
	go s.scheduleLoop()
	return nil
}

func (s *Service) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) SaveSchedule(appID string, spec registry.AppScheduleSpec) (registry.AppSchedule, error) {
	if (spec.Enabled == nil || *spec.Enabled) && (spec.Title != "" || spec.Body != "") {
		app, err := s.db.App(appID)
		if err != nil {
			return registry.AppSchedule{}, err
		}
		if len(s.Channels()) == 0 {
			return registry.AppSchedule{}, fmt.Errorf("%w: configure a notification channel before enabling a reminder", registry.ErrInvalid)
		}
		if app.NotificationsMuted {
			return registry.AppSchedule{}, fmt.Errorf("%w: notifications are muted for this app", registry.ErrInvalid)
		}
	}
	job, err := s.db.SaveAppSchedule(appID, spec)
	if err == nil {
		s.Wake()
	}
	return job, err
}

func (s *Service) DeleteSchedule(appID, id string) error {
	if err := s.db.DeleteAppSchedule(appID, id); err != nil {
		return err
	}
	s.Wake()
	return nil
}

func (s *Service) scheduleLoop() {
	defer s.background.Done()
	for s.ctx.Err() == nil {
		jobs, err := s.db.AllAppSchedules()
		delay := 24 * time.Hour
		if err != nil {
			delay = time.Minute
		}
		now := time.Now().UTC()
		chatRuns := 0
		for _, job := range jobs {
			if job.ChatID != "" && job.State == "running" {
				chatRuns++
			}
		}
		for _, job := range jobs {
			if job.State == "running" {
				if job.ChatID != "" && job.RunID != "" {
					delay = min(delay, 15*time.Second)
					s.reconcileChatSchedule(job)
					continue
				}
				delay = min(delay, time.Minute)
				if expiry, err := time.Parse(time.RFC3339Nano, job.LeaseUntil); err != nil || !expiry.After(now) {
					if job.ChatID == "" || !job.Active {
						if err = s.db.FinishAppSchedule(job.ID, job.Occurrence, errors.New("the computer restarted during this run"), true); err == nil {
							delay = min(delay, time.Second)
						}
						continue
					}
					// Conversation starts have durable receipts. An expired claim
					// with no attached run can safely retry that same occurrence.
				} else {
					continue
				}
			}
			if !job.Active {
				continue
			}
			// A report releases its admission worker after starting, but still
			// counts toward the host's bounded scheduled-turn concurrency.
			if job.ChatID != "" && job.State != "running" && chatRuns >= cap(s.workers) {
				delay = min(delay, 15*time.Second)
				continue
			}
			delay = min(delay, time.Minute)
			dueAt := job.NextRunAt
			if job.RetryAt != "" {
				dueAt = job.RetryAt
			}
			due, err := time.Parse(time.RFC3339Nano, dueAt)
			if err != nil {
				continue
			}
			if due.After(now) {
				delay = min(delay, due.Sub(now))
				continue
			}
			select {
			case s.workers <- struct{}{}:
			default:
				delay = min(delay, time.Second)
				continue
			}
			done, err := s.admit()
			if err != nil {
				<-s.workers
				delay = min(delay, time.Second)
				continue
			}
			claimed, err := s.db.ClaimAppSchedule(job.ID, job.NextRunAt)
			if err != nil {
				done()
				<-s.workers
				continue
			}
			if job.ChatID != "" && job.State != "running" {
				chatRuns++
			}
			s.background.Add(1)
			go func() { defer done(); s.runSchedule(claimed) }()
		}
		timer := time.NewTimer(delay)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-s.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (s *Service) runSchedule(job registry.AppSchedule) {
	defer s.background.Done()
	defer func() { <-s.workers; s.Wake() }()
	ctx, cancel := context.WithTimeout(s.ctx, 45*time.Second)
	defer cancel()
	if job.ChatID != "" {
		if s.chatRuntime == nil {
			s.finishSchedule(job, errors.New("scheduled conversations are unavailable"))
			return
		}
		if err := ctx.Err(); err != nil {
			return
		}
		run, err := s.chatRuntime.StartScheduledTurn(job.ChatID, job.Prompt, registry.ScheduleEventID(job))
		if err != nil {
			s.finishSchedule(job, err)
			return
		}
		if err := s.db.AttachScheduleRun(job.ID, job.Occurrence, run.ID); err != nil && !errors.Is(err, registry.ErrNotFound) {
			log.Printf("schedules: could not attach run %s to %s: %v", run.ID, job.ID, err)
		}
		return
	}
	err := s.executeSchedule(ctx, job)
	s.finishSchedule(job, err)
}

func (s *Service) reconcileChatSchedule(job registry.Schedule) {
	if s.chatRuntime == nil {
		return
	}
	run, ok := s.chatRuntime.ScheduledRun(job.RunID)
	if ok && run.Status == "running" {
		return
	}
	var err error
	if !ok {
		err = errors.New("the scheduled run is no longer available")
	} else if run.Status != "done" {
		err = fmt.Errorf("scheduled run %s", run.Status)
		if run.Error != "" {
			err = errors.New(run.Error)
		}
	}
	s.finishSchedule(job, err)
}

func (s *Service) finishSchedule(job registry.Schedule, err error) {
	for attempt := 0; attempt < 3; attempt++ {
		finishErr := s.db.FinishAppSchedule(job.ID, job.Occurrence, err, false)
		if finishErr == nil || errors.Is(finishErr, registry.ErrNotFound) || errors.Is(finishErr, registry.ErrConflict) {
			return
		}
		if attempt == 2 {
			log.Printf("apps: could not persist schedule outcome for %s: %v", job.ID, finishErr)
			return
		}
		// Only retry the SQLite write, never the command or a successful delivery.
		timer := time.NewTimer(time.Duration(attempt+1) * 100 * time.Millisecond)
		<-timer.C
	}
}

func (s *Service) executeSchedule(ctx context.Context, job registry.AppSchedule) error {
	app, err := s.db.App(job.AppID)
	if err != nil {
		return err
	}
	if app.Archived {
		return errors.New("this app was removed from Apps")
	}
	directory, err := s.db.AppDirectory(app)
	if err != nil {
		return err
	}
	if job.Command != "" && !job.CommandDone {
		result, err := s.exec.Exec(ctx, workspaceexec.ExecRequest{Argv: []string{"sh", "-lc", job.Command}, Directory: directory, TimeoutMS: 30000}, nil)
		if err != nil {
			return fmt.Errorf("scheduled command did not finish: %w", err)
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("scheduled command failed (exit %d)", result.ExitCode)
		}
	}
	if job.Command != "" {
		if err := s.db.AppScheduleCommandDone(job.ID, job.Occurrence); err != nil {
			return err
		}
	}
	if job.Title != "" || job.Body != "" {
		return s.Notify(ctx, app.ID, NotificationMessage{EventID: registry.ScheduleEventID(job), Title: job.Title, Body: job.Body})
	}
	return nil
}
