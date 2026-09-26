package projectsync

import (
	"bytes"
	"context"
	"fmt"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func (s *Service) dataEntry(ctx context.Context, data []byte, cwd string) (*Entry, error) {
	if data == nil {
		return nil, nil
	}
	var b bytes.Buffer
	if err := remapJSON(bytes.NewReader(data), &b, cwd, projectMarker); err != nil {
		return nil, err
	}
	e, err := s.blob(ctx, &b)
	e.JSONLines = true
	return &e, err
}
func (s *Service) readResource(ctx context.Context, r Resource, cwd string) (*Entry, error) {
	p, ok := s.porters[r.Harness].(agent.SessionDataPorter)
	if !ok || !p.ValidSessionDataName(r.Name) {
		return nil, fmt.Errorf("unsupported native memory artifact")
	}
	b, err := p.ReadSessionData(ctx, cwd, r.SessionID, r.Name)
	if err != nil {
		return nil, err
	}
	return s.dataEntry(ctx, b, cwd)
}
func (s *Service) installResource(ctx context.Context, w Write) error {
	r := w.Resource
	p, ok := s.porters[r.Harness].(agent.SessionDataPorter)
	if !ok {
		return fmt.Errorf("unsupported native memory adapter")
	}
	var before, after []byte
	var err error
	if w.Before != nil {
		before, err = s.expanded(*w.Before, w.CWD)
		if err != nil {
			return err
		}
	}
	if w.After != nil {
		after, err = s.expanded(*w.After, w.CWD)
		if err != nil {
			return err
		}
	}
	return p.ApplySessionData(ctx, w.CWD, r.SessionID, r.Name, before, after)
}
