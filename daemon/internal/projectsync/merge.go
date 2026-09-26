package projectsync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/oblien/mindwire/daemon/internal/artifact"
	"github.com/oblien/mindwire/daemon/internal/session"
)

func entry(m map[string]Entry, key string) *Entry {
	if e, ok := m[key]; ok {
		return &e
	}
	return nil
}
func choose[T any](base, local, incoming T) (T, bool) {
	if equal(local, incoming) || equal(incoming, base) {
		return local, true
	}
	if equal(local, base) {
		return incoming, true
	}
	return local, false
}

func (s *Service) commonAncestor(local, incoming string) (string, error) {
	if local == "" {
		return "", nil
	}
	walk := func(start string) (map[string]int, error) {
		seen := map[string]int{}
		queue := []string{start}
		for len(queue) > 0 {
			next := queue[0]
			queue = queue[1:]
			depth := seen[next]
			m, err := s.manifest(next)
			if err != nil {
				return nil, err
			}
			for _, p := range m.Parents {
				if _, ok := seen[p]; !ok {
					seen[p] = depth + 1
					queue = append(queue, p)
				}
			}
			if len(seen) > 100000 {
				return nil, invalid("checkpoint history is too large")
			}
		}
		seen[start] = 0
		return seen, nil
	}
	a, err := walk(local)
	if err != nil {
		return "", err
	}
	b, err := walk(incoming)
	if err != nil {
		return "", err
	}
	best := ""
	distance := int(^uint(0) >> 1)
	for id, ad := range a {
		if bd, ok := b[id]; ok && (ad+bd < distance || ad+bd == distance && id < best) {
			best, distance = id, ad+bd
		}
	}
	return best, nil
}

func (s *Service) mergeEntry(ctx context.Context, key string, base, local, incoming *Entry) (*Entry, bool, error) {
	if e, ok := choose(base, local, incoming); ok {
		return e, true, nil
	}
	if local == nil || incoming == nil || base == nil || local.Kind != "file" || incoming.Kind != "file" || base.Kind != "file" {
		return local, false, nil
	}
	mode, ok := choose(base.Mode, local.Mode, incoming.Mode)
	if !ok {
		return local, false, nil
	}
	if local.Size > 8<<20 || incoming.Size > 8<<20 || base.Size > 8<<20 {
		return local, false, nil
	}
	b, err := s.read(*base, 8<<20)
	if err != nil {
		return nil, false, err
	}
	l, err := s.read(*local, 8<<20)
	if err != nil {
		return nil, false, err
	}
	r, err := s.read(*incoming, 8<<20)
	if err != nil {
		return nil, false, err
	}
	var merged []byte
	if strings.HasPrefix(key, "records/") {
		merged, ok, err = mergeRecorded(b, l, r)
		if err != nil || !ok {
			return local, ok, err
		}
		var normalized bytes.Buffer
		if err = remapJSON(bytes.NewReader(merged), &normalized, projectMarker, projectMarker); err != nil {
			return nil, false, err
		}
		merged = normalized.Bytes()
	} else if local.JSONLines || incoming.JSONLines {
		// Native transcript records have order and identity. Only a strict
		// continuation is safe; text merging two continuations corrupts context.
		if bytes.HasPrefix(l, r) {
			merged = l
		} else if bytes.HasPrefix(r, l) {
			merged = r
		} else {
			return local, false, nil
		}
	} else {
		for _, v := range [][]byte{b, l, r} {
			if bytes.IndexByte(v, 0) >= 0 || !utf8.Valid(v) {
				return local, false, nil
			}
		}
		dir, err := os.MkdirTemp(s.root, "merge-")
		if err != nil {
			return nil, false, err
		}
		defer os.RemoveAll(dir)
		for name, data := range map[string][]byte{"base": b, "local": l, "incoming": r} {
			if err = os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
				return nil, false, err
			}
		}
		cmd := exec.CommandContext(ctx, "git", "merge-file", "-p", "--", filepath.Join(dir, "local"), filepath.Join(dir, "base"), filepath.Join(dir, "incoming"))
		merged, err = cmd.Output()
		if err != nil {
			if exit, yes := err.(*exec.ExitError); yes && exit.ExitCode() == 1 {
				return local, false, nil
			}
			return local, false, fmt.Errorf("could not compare independent text changes")
		}
	}
	e, err := s.blob(ctx, bytes.NewReader(merged))
	if err != nil {
		return nil, false, err
	}
	e.Mode = mode
	e.JSONLines = local.JSONLines
	return &e, true, nil
}

func mergeRecorded(b, l, r []byte) ([]byte, bool, error) {
	var base, local, incoming session.PortableChat
	for _, item := range []struct {
		b []byte
		v *session.PortableChat
	}{{b, &base}, {l, &local}, {r, &incoming}} {
		if err := json.Unmarshal(item.b, item.v); err != nil {
			return nil, false, err
		}
	}
	title, ok := choose(base.Title, local.Title, incoming.Title)
	if !ok {
		return nil, false, nil
	}
	local.Title = title
	for _, in := range incoming.Messages {
		found := false
		for i, current := range local.Messages {
			if current.ID != in.ID {
				continue
			}
			found = true
			var prior session.Message
			for _, p := range base.Messages {
				if p.ID == in.ID {
					prior = p
					break
				}
			}
			chosen, ok := choose(prior, current, in)
			if !ok {
				return nil, false, nil
			}
			local.Messages[i] = chosen
			break
		}
		if !found {
			local.Messages = append(local.Messages, in)
		}
	}
	for _, in := range incoming.Runs {
		found := false
		for i, current := range local.Runs {
			if current.ID != in.ID {
				continue
			}
			found = true
			var prior session.Run
			for _, p := range base.Runs {
				if p.ID == in.ID {
					prior = p
					break
				}
			}
			chosen, ok := choose(prior, current, in)
			if !ok {
				return nil, false, nil
			}
			local.Runs[i] = chosen
			break
		}
		if !found {
			local.Runs = append(local.Runs, in)
		}
	}
	sort.SliceStable(local.Messages, func(i, j int) bool { return local.Messages[i].CreatedAt < local.Messages[j].CreatedAt })
	sort.SliceStable(local.Runs, func(i, j int) bool { return local.Runs[i].CreatedAt < local.Runs[j].CreatedAt })
	local.ForkPending, ok = choose(base.ForkPending, local.ForkPending, incoming.ForkPending)
	if !ok {
		return nil, false, nil
	}
	out, err := json.Marshal(local)
	return out, true, err
}

func (s *Service) merge(ctx context.Context, base, local, incoming Manifest) (Manifest, []Conflict, error) {
	out := clone(local)
	out.Parents = []string{}
	out.CreatedAt = now()
	out.Entries = map[string]Entry{}
	out.Chats = map[string]Chat{}
	conflicts := []Conflict{}
	conflict := func(path, kind, message string) { conflicts = append(conflicts, Conflict{path, kind, message}) }
	if len(incoming.Artifacts) > 0 && out.Artifacts == nil {
		out.Artifacts = map[string]artifact.Record{}
	}
	for id, row := range incoming.Artifacts {
		if old, ok := out.Artifacts[id]; ok && !equal(old, row) {
			conflict("artifacts/"+id, "conversation", "The tool image changed independently.")
		} else {
			out.Artifacts[id] = row
		}
	}
	keys := map[string]bool{}
	for _, m := range []Manifest{base, local, incoming} {
		for key := range m.Entries {
			keys[key] = true
		}
	}
	for key := range keys {
		b, l, r := entry(base.Entries, key), entry(local.Entries, key), entry(incoming.Entries, key)
		// Chat deletion is local. Sync never erases the other copy's history.
		if !strings.HasPrefix(key, "files/") && r == nil {
			r = b
			if r == nil {
				r = l
			}
		}
		if !strings.HasPrefix(key, "files/") && l == nil {
			l = b
		}
		e, ok, err := s.mergeEntry(ctx, key, b, l, r)
		if err != nil {
			return out, nil, err
		}
		if !ok {
			kind := "file"
			message := "Changed in both workspaces. Both versions are preserved."
			if strings.HasPrefix(key, "native/") || strings.HasPrefix(key, "data/") || strings.HasPrefix(key, "records/") {
				kind = "conversation"
				message = "This conversation or its memory continued independently. Both histories are preserved."
			}
			conflict(key, kind, message)
		}
		if e != nil {
			out.Entries[key] = *e
		}
	}
	// A new local child keeps its directory even when the incoming copy removed
	// the old directory. Never recursively remove an unscanned/local child.
	for key := range out.Entries {
		if !strings.HasPrefix(key, "files/") {
			continue
		}
		for parent := filepath.ToSlash(filepath.Dir(key)); parent != "files" && parent != "."; parent = filepath.ToSlash(filepath.Dir(parent)) {
			if e, ok := out.Entries[parent]; ok {
				if e.Kind != "directory" {
					conflict(parent, "file", "A file and directory were created at the same path.")
				}
				continue
			}
			for _, m := range []Manifest{local, incoming, base} {
				if e, ok := m.Entries[parent]; ok && e.Kind == "directory" {
					out.Entries[parent] = e
					break
				}
			}
		}
	}
	chatIDs := map[string]bool{}
	for id := range local.Chats {
		chatIDs[id] = true
	}
	for id := range incoming.Chats {
		chatIDs[id] = true
	}
	for id := range chatIDs {
		l, lok := local.Chats[id]
		r, rok := incoming.Chats[id]
		b := base.Chats[id]
		if !lok {
			out.Chats[id] = r
			continue
		}
		if !rok {
			out.Chats[id] = l
			continue
		}
		if l.Harness != r.Harness {
			conflict("chat/"+id, "conversation", "The conversation harness changed independently.")
			continue
		}
		var ok bool
		l.SessionID, ok = choose(b.SessionID, l.SessionID, r.SessionID)
		if !ok {
			conflict("chat/"+id, "conversation", "The conversation was forked independently.")
		}
		l.Title, ok = choose(b.Title, l.Title, r.Title)
		if !ok && l.TitleIsUserSet && r.TitleIsUserSet {
			conflict("chat/"+id, "conversation", "The conversation was renamed independently.")
		}
		if r.UpdatedAt > l.UpdatedAt {
			l.UpdatedAt = r.UpdatedAt
			l.NativeTitle = r.NativeTitle
		}
		l.TitleIsUserSet = l.TitleIsUserSet || r.TitleIsUserSet
		set := map[string]bool{}
		for _, name := range append(l.NativeFiles, r.NativeFiles...) {
			set[name] = true
		}
		l.NativeFiles = nil
		for name := range set {
			l.NativeFiles = append(l.NativeFiles, name)
		}
		sort.Strings(l.NativeFiles)
		out.Chats[id] = l
	}
	if name, ok := choose(base.Name, local.Name, incoming.Name); ok {
		out.Name = name
	}
	if icon, ok := choose(base.IconPath, local.IconPath, incoming.IconPath); ok {
		out.IconPath = icon
	}
	var gitConflicts []Conflict
	var err error
	out.Git, gitConflicts, err = s.mergeGit(ctx, base.Git, local.Git, incoming.Git)
	if err != nil {
		return out, nil, err
	}
	conflicts = append(conflicts, gitConflicts...)
	sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].Path < conflicts[j].Path })
	return out, conflicts, nil
}

func (s *Service) mergeGit(ctx context.Context, base, local, incoming *Git) (*Git, []Conflict, error) {
	if gitEqual(local, incoming) || gitEqual(incoming, base) {
		return local, nil, nil
	}
	if gitEqual(local, base) {
		return incoming, nil, nil
	}
	if incoming == nil {
		return local, nil, nil
	}
	if local == nil {
		return incoming, nil, nil
	}
	if base == nil {
		base = &Git{Refs: map[string]string{}, Index: map[string]Entry{}}
	}
	out := clone(local)
	out.Bundle = incoming.Bundle
	conflicts := []Conflict{}
	var ok bool
	out.Head, ok = choose(base.Head, local.Head, incoming.Head)
	if !ok {
		conflicts = append(conflicts, Conflict{"HEAD", "git", "The active branch changed in both workspaces."})
	}
	refs := map[string]bool{}
	for _, g := range []*Git{base, local, incoming} {
		for name := range g.Refs {
			refs[name] = true
		}
	}
	for name := range refs {
		v, ok := choose(base.Refs[name], local.Refs[name], incoming.Refs[name])
		if !ok {
			conflicts = append(conflicts, Conflict{name, "git", "This Git reference changed independently. Both commit histories are retained."})
		}
		if v == "" {
			delete(out.Refs, name)
		} else {
			out.Refs[name] = v
		}
	}
	paths := map[string]bool{}
	for _, g := range []*Git{base, local, incoming} {
		for name := range g.Index {
			paths[name] = true
		}
	}
	for path := range paths {
		e, ok, err := s.mergeEntry(ctx, "index/"+path, entry(base.Index, path), entry(local.Index, path), entry(incoming.Index, path))
		if err != nil {
			return out, nil, err
		}
		if !ok {
			conflicts = append(conflicts, Conflict{path, "git", "Staged changes conflict. Neither staging area has been replaced."})
		}
		if e == nil {
			delete(out.Index, path)
		} else {
			out.Index[path] = *e
		}
	}
	return out, conflicts, nil
}
