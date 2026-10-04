package projectsync

import (
	"context"
	"strings"
)

// Materialize a filtered view of history, keeping the full originals locally.
// Relay walks only this view: excluded blobs never hitch a ride in old parents.
func (s *Service) filteredHistory(ctx context.Context, id string, rules *fileSelection) (string, error) {
	seen := map[string]string{}
	visiting := map[string]bool{}
	var visit func(string) (string, error)
	visit = func(id string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if result, ok := seen[id]; ok {
			return result, nil
		}
		if visiting[id] || len(visiting)+len(seen) > 100000 {
			return "", invalid("invalid or oversized checkpoint history")
		}
		visiting[id] = true
		defer delete(visiting, id)
		original, err := s.manifest(id)
		if err != nil {
			return "", err
		}
		m := clone(original)
		for i, parent := range m.Parents {
			m.Parents[i], err = visit(parent)
			if err != nil {
				return "", err
			}
		}
		excluded, err := rules.excludedKnown(ctx, m)
		if err != nil {
			return "", err
		}
		m.Excluded = mask(append(m.Excluded, excluded...)).roots()
		omitted := mask(m.Excluded)
		for key := range m.Entries {
			if strings.HasPrefix(key, "files/") && omitted.covers(strings.TrimPrefix(key, "files/")) {
				delete(m.Entries, key)
			}
		}
		if equal(original, m) {
			seen[id] = id
			return id, nil
		}
		m.Version = Version
		if m.Origin == "" {
			m.Origin = id
		}
		if err = s.validateManifest(m); err != nil {
			return "", err
		}
		c, err := s.seal(ctx, m)
		if err != nil {
			return "", err
		}
		seen[id] = c.ID
		return c.ID, nil
	}
	return visit(id)
}

// Filtered views of one historical checkpoint may have different file scopes.
// Only content known to both views is a safe three-way merge base. Everything
// else is unknown, so independently created content conflicts instead of losing
// either version when a user changes their inclusion choices later.
func sharedBase(a, b Manifest) (Manifest, error) {
	left, right := clone(a), clone(b)
	for _, m := range []*Manifest{&left, &right} {
		m.Version = 0
		m.Parents = nil
		m.Origin = ""
		m.Excluded = nil
		m.Entries = nil
	}
	if !equal(left, right) {
		return Manifest{}, invalid("inconsistent filtered checkpoint history")
	}
	out := clone(b)
	unknown := mask(append(append([]string{}, a.Excluded...), b.Excluded...))
	for key, value := range out.Entries {
		other, ok := a.Entries[key]
		if ok && equal(value, other) {
			continue
		}
		if ok {
			return Manifest{}, invalid("filtered views disagree about historical file content")
		}
		if !strings.HasPrefix(key, "files/") {
			return Manifest{}, invalid("filtered history changed native conversation data")
		}
		delete(out.Entries, key)
		unknown[strings.TrimPrefix(key, "files/")] = true
	}
	for key := range a.Entries {
		if _, ok := b.Entries[key]; ok {
			continue
		}
		if !strings.HasPrefix(key, "files/") {
			return Manifest{}, invalid("filtered history omitted native conversation data")
		}
		unknown[strings.TrimPrefix(key, "files/")] = true
	}
	out.Excluded = unknown.roots()
	return out, nil
}
