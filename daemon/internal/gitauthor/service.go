package gitauthor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/oblien/mindwire/daemon/internal/registry"
)

const Version = 1

type Scope string

const (
	Repository    Scope = "repository"
	Workspace     Scope = "workspace"
	AllWorkspaces Scope = "all_workspaces"
)

type Settings struct {
	Effective       registry.GitIdentity  `json:"effective"`
	Repository      *registry.GitIdentity `json:"repository"`
	Workspace       *registry.GitIdentity `json:"workspace"`
	AllWorkspaces   *registry.GitIdentity `json:"allWorkspaces"`
	DefaultRevision string                `json:"defaultRevision"`
}

type Update struct {
	Scope     Scope                 `json:"scope"`
	Identity  *registry.GitIdentity `json:"identity"`
	RequestID string                `json:"requestId,omitempty"`
	// An app retains this until acknowledgement, including after a lost response.
	ExpectedRevision *string `json:"expectedRevision,omitempty"`
}

type Service struct{ store *registry.Store }

func New(store *registry.Store) *Service { return &Service{store: store} }

func (s *Service) config(scope Scope) string {
	return filepath.Join(s.store.Directory(), "git-author-"+string(scope)+".config")
}

func Validate(author *registry.GitIdentity) error {
	if author == nil {
		return nil
	}
	author.Name, author.Email = strings.TrimSpace(author.Name), strings.TrimSpace(author.Email)
	if author.Name == "" || len(author.Name) > 256 || len(author.Email) > 320 ||
		strings.Count(author.Email, "@") != 1 || strings.HasPrefix(author.Email, "@") || strings.HasSuffix(author.Email, "@") {
		return fmt.Errorf("%w: enter a valid Git author name and email", registry.ErrInvalid)
	}
	for _, v := range []string{author.Name, author.Email} {
		for _, c := range v {
			if unicode.IsControl(c) || c == '<' || c == '>' {
				return fmt.Errorf("%w: enter a valid Git author name and email", registry.ErrInvalid)
			}
		}
	}
	if strings.IndexFunc(author.Email, unicode.IsSpace) >= 0 {
		return fmt.Errorf("%w: enter a valid Git author email", registry.ErrInvalid)
	}
	return nil
}

func (s *Service) Read(ctx context.Context, directory string) (Settings, error) {
	var state Settings
	var err error
	defaults, err := configValues(ctx, "config", "--file", s.config(AllWorkspaces), "--no-includes")
	if err != nil {
		return state, err
	}
	state.AllWorkspaces = identityFrom(defaults)
	state.DefaultRevision = defaults["mindwire.authorrequest"]
	state.Workspace, err = identity(ctx, "config", "--file", s.config(Workspace), "--no-includes")
	if err != nil {
		return state, err
	}
	if directory == "" {
		if state.AllWorkspaces != nil {
			state.Effective = *state.AllWorkspaces
		}
		if state.Workspace != nil {
			state.Effective = *state.Workspace
		}
		return state, nil
	}
	state.Repository, err = identity(ctx, "-C", directory, "config", "--local", "--no-includes")
	if err != nil {
		return state, err
	}
	current, err := identity(ctx, "-C", directory, "config", "--includes")
	if current != nil {
		state.Effective = *current
	}
	return state, err
}

func (s *Service) Save(ctx context.Context, directory string, req Update) (Settings, error) {
	if req.Identity != nil {
		copy := *req.Identity
		req.Identity = &copy
	}
	if err := Validate(req.Identity); err != nil {
		return Settings{}, err
	}
	var config string
	switch req.Scope {
	case Repository:
		var err error
		config, err = repositoryConfig(ctx, directory)
		if err != nil {
			return Settings{}, err
		}
	case Workspace, AllWorkspaces:
		config = s.config(req.Scope)
		if req.Scope == AllWorkspaces && (req.RequestID == "" || len(req.RequestID) > 128 || req.ExpectedRevision == nil) {
			return Settings{}, fmt.Errorf("%w: a default author request ID and expected revision are required", registry.ErrInvalid)
		}
		// Includes are installed before publishing the default. Missing include
		// files are valid in Git, so a failed preparation does not publish half a save.
		if err := s.bind(ctx, directory, ""); err != nil {
			return Settings{}, err
		}
	default:
		return Settings{}, fmt.Errorf("%w: select repository, workspace or all_workspaces", registry.ErrInvalid)
	}
	if req.Scope == Repository && directory != "" {
		if err := s.PrepareRepository(ctx, directory); err != nil {
			return Settings{}, err
		}
	}
	err := editConfig(ctx, config, func(temp string) error {
		if req.Scope == AllWorkspaces {
			revision, err := value(ctx, []string{"config", "--file", temp}, "mindwire.authorRequest")
			if err != nil {
				return err
			}
			if revision == req.RequestID {
				current, err := identity(ctx, "config", "--file", temp)
				if err != nil {
					return err
				}
				if (current == nil) != (req.Identity == nil) || current != nil && *current != *req.Identity {
					return fmt.Errorf("%w: this author request was already used", registry.ErrConflict)
				}
				return nil
			}
			if revision != *req.ExpectedRevision {
				return fmt.Errorf("%w: the default author changed on this workspace; review it before replacing it", registry.ErrConflict)
			}
			if _, err := git(ctx, "config", "--file", temp, "mindwire.authorRequest", req.RequestID); err != nil {
				return err
			}
		}
		return writeIdentity(ctx, temp, req.Identity)
	})
	if err != nil {
		return Settings{}, err
	}
	return s.Read(ctx, directory)
}

func (s *Service) configured() bool {
	for _, scope := range []Scope{Workspace, AllWorkspaces} {
		if _, err := os.Stat(s.config(scope)); err == nil {
			return true
		}
	}
	return false
}
