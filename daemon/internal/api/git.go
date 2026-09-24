package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/gitaccess"
	"github.com/oblien/mindwire/daemon/internal/gitops"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

type projectGitState struct {
	Connection *gitaccess.Connection `json:"connection,omitempty"`
	Inherited  bool                  `json:"inherited"`
	Stored     bool                  `json:"stored"`
	RepoURL    string                `json:"repoUrl,omitempty"`
}

func (a *API) gitConnection(project *registry.Project, repo string) *gitaccess.Connection {
	var explicit *gitaccess.Connection
	if project != nil {
		explicit = project.GitConnection
	}
	return a.gitAccess.EffectiveConnection(explicit, repo)
}

func (a *API) workspaceGitState() gitaccess.State {
	state := a.gitAccess.State()
	if a.gitJobs != nil {
		state.ActiveOperations = a.gitJobs.ActiveCount()
	}
	return state
}

func (a *API) gitState(w http.ResponseWriter, r *http.Request) {
	if !a.requireGit(w) {
		return
	}
	writeJSON(w, http.StatusOK, a.workspaceGitState())
}

func (a *API) requireGit(w http.ResponseWriter) bool {
	if a.gitAccess != nil && a.gitJobs != nil {
		return true
	}
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "Git authentication requires an updated daemon"})
	return false
}

func gitError(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
}

func (a *API) gitDefault(w http.ResponseWriter, r *http.Request) {
	if !a.requireGit(w) {
		return
	}
	var req struct {
		Connection *gitaccess.Connection `json:"connection"`
		Auth       *gitaccess.Auth       `json:"auth,omitempty"`
	}
	if decode(w, r, &req) != nil {
		badRequest(w, "invalid Git connection")
		return
	}
	a.registryMu.Lock()
	defer a.registryMu.Unlock()
	snapshot, err := a.registry.Snapshot(nil)
	if err != nil {
		workspaceError(w, err)
		return
	}
	for _, project := range snapshot.Projects {
		if project.GitConnection == nil && a.BusyPath(project.Path) {
			workspaceError(w, registry.ErrConflict)
			return
		}
	}
	operations, err := a.projects.List(true)
	if err != nil {
		workspaceError(w, err)
		return
	}
	for _, operation := range operations {
		if operation.GitConnection == nil {
			workspaceError(w, registry.ErrConflict)
			return
		}
	}
	previous := a.gitAccess.State().Default
	if err := a.gitAccess.SetDefault(req.Connection, req.Auth); err != nil {
		gitError(w, err)
		return
	}
	var applied []registry.Project
	for _, project := range snapshot.Projects {
		if project.GitConnection != nil {
			continue
		}
		repo, err := gitaccess.RemoteURL(r.Context(), project.Path, false)
		if err == nil && repo == "" {
			continue
		}
		if err == nil {
			err = a.gitAccess.ConfigureRepository(r.Context(), project.Path, repo, a.gitConnection(&project, repo))
		}
		if err != nil {
			_ = a.gitAccess.RestoreDefault(previous)
			for _, old := range applied {
				repo, _ := gitaccess.RemoteURL(r.Context(), old.Path, false)
				_ = a.gitAccess.ConfigureRepository(r.Context(), old.Path, repo, a.gitConnection(&old, repo))
			}
			gitError(w, err)
			return
		}
		applied = append(applied, project)
	}
	writeJSON(w, http.StatusOK, a.workspaceGitState())
}

func (a *API) gitForget(w http.ResponseWriter, r *http.Request) {
	if !a.requireGit(w) {
		return
	}
	if err := a.gitAccess.Forget(r.PathValue("id")); err != nil {
		gitError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.workspaceGitState())
}

func (a *API) projectGit(w http.ResponseWriter, r *http.Request) {
	if !a.requireGit(w) {
		return
	}
	project, err := a.registry.Project(r.PathValue("id"))
	if err != nil {
		workspaceError(w, err)
		return
	}
	repo, err := gitaccess.RemoteURL(r.Context(), project.Path, false)
	if err != nil {
		gitError(w, err)
		return
	}
	connection := a.gitConnection(project, repo)
	stored := false
	if connection != nil {
		for _, id := range a.gitAccess.State().StoredConnectionIDs {
			stored = stored || id == connection.ID
		}
	}
	writeJSON(w, http.StatusOK, projectGitState{Connection: connection, Inherited: project.GitConnection == nil, Stored: stored, RepoURL: gitaccess.PublicRemoteURL(repo)})
}

func (a *API) projectGitSet(w http.ResponseWriter, r *http.Request) {
	if !a.requireGit(w) {
		return
	}
	var req struct {
		Connection       *gitaccess.Connection `json:"connection"`
		Auth             *gitaccess.Auth       `json:"auth,omitempty"`
		ExpectedRevision *int64                `json:"expectedRevision"`
	}
	if decode(w, r, &req) != nil {
		badRequest(w, "invalid Git connection")
		return
	}
	a.registryMu.Lock()
	defer a.registryMu.Unlock()
	project, err := a.registry.Project(r.PathValue("id"))
	if err != nil {
		workspaceError(w, err)
		return
	}
	if req.ExpectedRevision == nil || *req.ExpectedRevision != project.Revision || a.BusyPath(project.Path) {
		workspaceError(w, registry.ErrConflict)
		return
	}
	if err := a.registry.CheckProjectPath(project.Path); err != nil {
		workspaceError(w, err)
		return
	}
	if req.Connection != nil {
		if err = a.gitAccess.Save(*req.Connection, req.Auth); err != nil {
			gitError(w, err)
			return
		}
	}
	data, _ := json.Marshal(project)
	var record map[string]any
	_ = json.Unmarshal(data, &record)
	record["gitConnection"] = req.Connection // explicit null restores inheritance
	data, _ = json.Marshal(record)
	if err = a.projects.Save(project.ID, data, req.ExpectedRevision); err != nil {
		workspaceError(w, err)
		return
	}
	a.writeWorkspaceSnapshot(w)
}

func (a *API) projectGitRun(w http.ResponseWriter, r *http.Request) {
	if !a.requireGit(w) {
		return
	}
	var req gitOperationRequest
	if r.ContentLength != 0 && decode(w, r, &req) != nil {
		badRequest(w, "invalid Git authorization")
		return
	}
	req.ID, req.Action = rand.Text(), r.PathValue("operation")
	if !gitops.Network(req.Action) {
		badRequest(w, "unsupported Git operation")
		return
	}
	o, err := a.startGitOperation(r.Context(), r.PathValue("id"), req)
	if err != nil {
		workspaceError(w, err)
		return
	}
	// Compatibility response for older clients; the accepted job has the same
	// durable lifecycle as the new API and survives this observer disconnecting.
	o, err = a.gitJobs.Wait(r.Context(), o.ID)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		workspaceError(w, err)
		return
	}
	if o.Status != "succeeded" {
		gitError(w, errors.New(o.Error))
		return
	}
	writeJSON(w, http.StatusOK, gitaccess.Result{Output: o.Output})
}

type gitOperationRequest struct {
	ID       string                `json:"id"`
	Action   string                `json:"action"`
	Paths    []string              `json:"paths,omitempty"`
	Message  string                `json:"message,omitempty"`
	Branch   string                `json:"branch,omitempty"`
	Remote   bool                  `json:"remote,omitempty"`
	Identity *registry.GitIdentity `json:"identity,omitempty"`
	Auth     *gitaccess.Auth       `json:"auth,omitempty"`
}

func (a *API) startGitOperation(ctx context.Context, projectID string, req gitOperationRequest) (registry.GitOperation, error) {
	spec := registry.GitSpec{ID: req.ID, ProjectID: projectID, Action: req.Action, Paths: req.Paths, Message: req.Message,
		Branch: req.Branch, Remote: req.Remote, Identity: req.Identity}
	if err := gitops.Normalize(&spec); err != nil {
		return registry.GitOperation{}, err
	}
	a.registryMu.Lock()
	defer a.registryMu.Unlock()
	if old, err := a.gitJobs.Get(spec.ID); err == nil {
		if !gitops.SameIntent(old.GitSpec, spec) {
			return old, registry.ErrConflict
		}
		return old, nil
	} else if !errors.Is(err, registry.ErrNotFound) {
		return registry.GitOperation{}, err
	}
	project, err := a.registry.Project(projectID)
	if err != nil {
		return registry.GitOperation{}, err
	}
	spec.Path, err = gitops.Root(ctx, project.Path)
	if err != nil {
		return registry.GitOperation{}, err
	}
	if a.BusyPath(spec.Path) {
		return registry.GitOperation{}, registry.ErrConflict
	}
	if err := a.registry.CheckProjectPath(spec.Path); err != nil {
		return registry.GitOperation{}, err
	}
	var connection *gitaccess.Connection
	if gitops.Network(req.Action) {
		repo, err := gitaccess.RemoteURL(ctx, spec.Path, false)
		if err != nil {
			return registry.GitOperation{}, err
		}
		connection = a.gitConnection(project, repo)
	} else if req.Auth != nil && *req.Auth != (gitaccess.Auth{}) {
		return registry.GitOperation{}, fmt.Errorf("%w: local Git operations do not accept credentials", registry.ErrInvalid)
	}
	return a.gitJobs.Start(spec, connection, req.Auth)
}

func (a *API) Busy(chatID string) bool { return a.sup.Busy(chatID) }
func (a *API) BusyPath(path string) bool {
	canonical, err := workspacepath.WorkingDirectory(path)
	if err != nil {
		return true
	}
	return a.sup.BusyPath(canonical) || a.gitBusyPath(canonical)
}
func (a *API) gitBusyPath(path string) bool {
	return a.gitJobs != nil && a.gitJobs.BusyPath(path)
}

// Runs resolve the authoritative project at admission. Credentials never become
// part of the user message, transcript, run record, or generic agent settings.
func (a *API) prepareGitRun(ctx context.Context, chatID, cwd string, auth *gitaccess.Auth) (*gitaccess.Lease, error) {
	var project *registry.Project
	if chatID != "" {
		_, _, saved, err := a.registry.ChatContext(chatID)
		if err != nil {
			return nil, err
		}
		project = saved
	}
	if project != nil {
		cwd = project.Path
	}
	if cwd == "" {
		cwd = a.sup.CWD()
	}
	if project == nil {
		path, err := workspacepath.WorkingDirectory(cwd)
		if err != nil {
			return nil, err
		}
		snapshot, err := a.registry.Snapshot(nil)
		if err != nil {
			return nil, err
		}
		for _, candidate := range snapshot.Projects {
			relative, err := filepath.Rel(candidate.Path, path)
			if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
				(project == nil || len(candidate.Path) > len(project.Path)) {
				value := candidate
				project = &value
			}
		}
	}
	if a.gitBusyPath(cwd) {
		return nil, registry.ErrConflict
	}
	if c := a.gitConnection(project, ""); c == nil || c.Mode == "native" {
		return a.gitAccess.Prepare(ctx, "", c, auth)
	}
	repo, err := gitaccess.RemoteURL(ctx, cwd, false)
	if err != nil {
		return nil, err
	}
	if repo == "" {
		return &gitaccess.Lease{Close: func() {}}, nil
	}
	connection := a.gitConnection(project, repo)
	return a.gitAccess.Prepare(ctx, repo, connection, auth)
}

func isGitError(err error) bool { return errors.Is(err, gitaccess.ErrCredential) }
