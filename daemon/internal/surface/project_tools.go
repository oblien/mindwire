package surface

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const projectDesktopInstructions = `Mindwire provides a saved desktop for this project on its Oblien VM. For UI work call surface_project_desktop with a stable requestId and mode "control". The service asks the user for desktop access, creates or reuses the project's desktop, and starts it if stopped. The user can watch the same desktop in the app.
Use surface_capture for a PNG screenshot and fresh frameId; use surface_action for pixel clicks, double clicks (count:2), drags, scrolling, keyboard chords and text. Pass sessionId, the controller generation, a unique stable requestId, and a frameId younger than 30 seconds for pointer input. Capture again after actions to verify the result. On Linux use Alt+F2 to open the desktop's Run dialog, type a command such as "xdg-open http://localhost:3000", and press Return to open a browser in this exact GUI session. Start the project's web server with the harness's normal command tool. Never guess DISPLAY or launch a separate VNC server. Desktop profiles share workspace files; they are not security sandboxes.
Use surface_release when finished. It leaves apps, the profile and files running for reconnection. Do not stop/delete a desktop just because a task ends. A user can take control at any time; stop input if control is revoked or busy. Never blindly repeat an outcome_unknown action. Provider credentials are private to Mindwire and are never needed by the harness. Only this run can use its desktop control sessions.`

func (s *Service) desktopInstructions(actor Actor) string {
	if !s.HasDesktopCatalog() {
		return "Use Mindwire's desktop tools to request access, capture the screen, send input and release control. Do not bypass a busy controller."
	}
	return projectDesktopInstructions + fmt.Sprintf("\nCurrent project ID: %q. Desktop selection is restricted to this project's saved desktops.", actor.ProjectID)
}

// Shell-capable harnesses without per-turn MCP support use the same MCP service
// through the daemon helper. Nothing is written into their global config.
func FallbackInstructions() string {
	return "\n\n[Mindwire workspace tools]\n" + projectDesktopInstructions + `
This harness can call the tools using "$MINDWIRE_DESKTOP_HELPER" --desktop-tool TOOL_NAME 'JSON_ARGUMENTS'. Use --desktop-tool list to inspect the full schemas. The helper reads this run's private authorization from its environment; never print it. Capture returns imagePath: open that PNG using your native image tool. Example: "$MINDWIRE_DESKTOP_HELPER" --desktop-tool surface_project_desktop '{"requestId":"ui-work-1","mode":"control"}'.` + "\n[/Mindwire workspace tools]"
}

func (s *Service) openForRun(ctx context.Context, actor Actor, in OpenRequest) (*mcp.CallToolResult, any, error) {
	if !s.HasDesktopCatalog() {
		session, err := s.Open(ctx, actor, in)
		return toolResult(toolPayload{SurfaceID: s.surfaceID, Operation: "open", Session: &session}, nil, err)
	}
	payload := toolPayload{SurfaceID: DesktopID, Operation: "open"}
	if !validRequest(in.RequestID) || (in.Mode != "view" && in.Mode != "control") {
		return toolResult(payload, nil, problem("invalid_request", "Provide a stable requestId and choose view or control."))
	}
	if actor.ProjectID == "" {
		return toolResult(payload, nil, problem("project_required", "Open a project chat before using a project desktop."))
	}
	key := actor.Kind + ":" + actor.RunID + ":" + in.RequestID
	s.mu.Lock()
	if pending, exists := s.projectOpens[key]; exists {
		s.mu.Unlock()
		if pending != in {
			return toolResult(payload, nil, problem("request_conflict", "This request ID is already opening a different desktop session."))
		}
		return toolResult(payload, nil, problem("approval_pending", "This desktop request is still being prepared or waiting for approval."))
	}
	if len(s.projectOpens) >= 32 {
		s.mu.Unlock()
		return toolResult(payload, nil, problem("session_limit", "Wait for a pending desktop request to finish."))
	}
	if s.projectOpens == nil {
		s.projectOpens = map[string]OpenRequest{}
	}
	s.projectOpens[key] = in
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.projectOpens, key); s.mu.Unlock() }()
	// Completed and in-flight retries share the same approval/creation intent.
	for _, child := range s.desktopChildren() {
		child.mu.Lock()
		id := child.openIDs[key]
		child.mu.Unlock()
		if id != "" {
			if in.DesktopID != "" && in.DesktopID != child.surfaceID {
				return toolResult(payload, nil, problem("request_conflict", "This request ID opened another desktop."))
			}
			session, err := child.sessionCopy(id, actor)
			return toolResult(toolPayload{SurfaceID: child.surfaceID, DesktopID: child.surfaceID, Operation: "open", Session: &session}, nil, err)
		}
	}
	if in.DesktopID != "" {
		links, err := s.desktopLinks(actor.ProjectID)
		if err != nil {
			return toolResult(payload, nil, err)
		}
		if !links[in.DesktopID] {
			return toolResult(payload, nil, problem("forbidden", "Choose a desktop linked to the current project."))
		}
	}
	s.mu.Lock()
	approve := s.approval
	s.mu.Unlock()
	if approve == nil {
		return toolResult(payload, nil, problem("approval_required", "Approve desktop access for this agent run."))
	}
	if err := approve(ctx, actor, "Allow "+actor.Name+" to create or open this project's desktop and control it for this session?"); err != nil {
		return toolResult(payload, nil, err)
	}
	var desktop SavedDesktop
	var err error
	if in.DesktopID == "" {
		desktop, err = s.EnsureProjectDesktop(ctx, actor.ProjectID)
	} else {
		var result DesktopResult
		result, err = s.DesktopOperation(ctx, in.DesktopID, "get", nil)
		desktop = result.Session
		if err == nil && (desktop.State == "stopped" || desktop.State == "failed") {
			result, err = s.DesktopOperation(ctx, in.DesktopID, "start", nil)
			desktop = result.Session
		}
	}
	if err != nil {
		return toolResult(payload, nil, err)
	}
	payload.DesktopID = desktop.ID
	payload.SurfaceID = desktop.ID
	payload.Desktop = &desktop
	deadline := time.NewTimer(45 * time.Second)
	defer deadline.Stop()
	for !desktop.Available {
		if desktop.State == "failed" || desktop.State == "deleting" {
			return toolResult(payload, nil, problem("unavailable", "The project desktop did not start. Check its status in the app."))
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return toolResult(payload, nil, ctx.Err())
		case <-deadline.C:
			timer.Stop()
			return toolResult(payload, nil, problem("preparing", "The desktop is saved and still starting. Retry to reconnect to this same desktop."))
		case <-timer.C:
		}
		result, err := s.DesktopOperation(ctx, desktop.ID, "get", nil)
		if err != nil {
			return toolResult(payload, nil, err)
		}
		desktop = result.Session
	}
	selected, err := s.ForDesktop(desktop.ID)
	if err != nil {
		return toolResult(payload, nil, err)
	}
	actor.desktopApproved = true
	in.DesktopID = desktop.ID
	session, err := selected.Open(ctx, actor, in)
	payload.Session = &session
	return toolResult(payload, nil, err)
}
