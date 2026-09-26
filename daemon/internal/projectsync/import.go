package projectsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

func (s *Service) recordEntry(ctx context.Context, data session.PortableChat, cwd string) (Entry, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return Entry{}, err
	}
	var normalized bytes.Buffer
	if err = remapJSON(bytes.NewReader(b), &normalized, cwd, projectMarker); err != nil {
		return Entry{}, err
	}
	e, err := s.blob(ctx, &normalized)
	e.JSONLines = true
	return e, err
}

func (s *Service) target(req Request, incoming Manifest) (registry.Project, error) {
	snap, err := s.reg.Snapshot(nil)
	if err != nil {
		return registry.Project{}, err
	}
	for _, p := range snap.Projects {
		if p.SyncID == incoming.ProjectID {
			if req.Path != "" {
				path, err := workspacepath.Canonical(req.Path)
				if err != nil {
					return p, err
				}
				actual, err := workspacepath.Canonical(p.Path)
				if err != nil {
					return p, err
				}
				if path != actual {
					return p, invalid("this workspace already has a copy of the project; use its existing location")
				}
			}
			p.Path, err = workspacepath.Canonical(p.Path)
			return p, err
		}
	}
	path, err := workspacepath.Canonical(req.Path)
	if err != nil {
		return registry.Project{}, invalid("choose a destination folder for the first copy")
	}
	if filepath.Dir(path) == path {
		return registry.Project{}, invalid("select a project directory, not the filesystem root")
	}
	for _, p := range snap.Projects {
		actual, err := workspacepath.Canonical(p.Path)
		if err != nil {
			continue
		}
		if actual == path {
			if p.SyncID != "" {
				return p, invalid("this folder belongs to a different synchronized project")
			}
			p.SyncID = incoming.ProjectID
			p.Path = path
			return p, nil
		}
		if workspacepath.Overlaps(actual, path) {
			return registry.Project{}, invalid("the destination overlaps another registered project")
		}
	}
	return registry.Project{Record: registry.Record{ID: "sync-" + registry.SyncKey(snap.WorkspaceID, "project", incoming.ProjectID)[:32], WorkspaceID: snap.WorkspaceID, CreatedAt: now()}, SyncID: incoming.ProjectID, Name: incoming.Name, Path: path, RepoURL: incoming.RepoURL, IconPath: incoming.IconPath}, nil
}

func (s *Service) importCheckpoint(ctx context.Context, o *Operation) error {
	incoming, err := s.manifest(o.CheckpointID)
	if err != nil {
		return err
	}
	if err = s.validateManifest(incoming); err != nil {
		return err
	}
	p, err := s.target(o.Request, incoming)
	if err != nil {
		return err
	}
	if err = nativeIdle(ctx, p.Path); err != nil {
		return err
	}
	if _, err = s.reg.Project(p.ID); err == nil && s.discover != nil {
		if err = s.discover(ctx, p.ID); err != nil {
			return err
		}
	}
	if err = s.reserve(o, p); err != nil {
		return err
	}
	keepLock := false
	defer func() {
		if !keepLock {
			_ = s.reg.ReleaseSync(o.ID)
		}
	}()
	o.Status, o.Phase = "preparing", "compare"
	if err = s.save(*o); err != nil {
		return err
	}
	local, err := s.captureProject(ctx, p)
	if err != nil {
		return err
	}
	var head string
	if err = s.reg.SyncGet("head", p.SyncID, &head); err != nil && !errors.Is(err, registry.ErrNotFound) {
		return err
	}
	baseID, err := s.commonAncestor(head, o.CheckpointID)
	if err != nil {
		return err
	}
	base := Manifest{Version: Version, ProjectID: p.SyncID, Entries: map[string]Entry{}, Chats: map[string]Chat{}}
	if baseID != "" {
		base, err = s.manifest(baseID)
		if err != nil {
			return err
		}
	}
	local.Parents = []string{}
	if head != "" {
		local.Parents = append(local.Parents, head)
	}
	backup, err := s.seal(ctx, local)
	if err != nil {
		return err
	}
	o.RecoveryCheckpointID = backup.ID
	merged, conflicts, err := s.merge(ctx, base, local, incoming)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		o.Status, o.Phase, o.Conflicts = "conflicts", "conflicts", conflicts
		return s.save(*o)
	}
	if merged.Git != nil && local.Git != nil && incoming.Git != nil {
		merged.Git.Bundle, err = s.unionBundle(ctx, merged.Git, local.Git, incoming.Git)
		if err != nil {
			return err
		}
	}
	merged.Parents = []string{o.CheckpointID, backup.ID}
	if o.CheckpointID == backup.ID {
		merged.Parents = merged.Parents[:1]
	}
	if err = s.validateManifest(merged); err != nil {
		return err
	}
	if err = s.preflightGit(ctx, merged.Git); err != nil {
		return err
	}
	writes, conflicts, err := s.planWrites(ctx, p.Path, local, merged)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		o.Status, o.Phase, o.Conflicts = "conflicts", "conflicts", conflicts
		return s.save(*o)
	}
	// Last full stability check before the write-ahead journal is published.
	check, err := s.captureProject(ctx, p)
	if err != nil {
		return err
	}
	if !sameSnapshot(local, check) {
		return fmt.Errorf("destination changed while comparing; retry when work is saved")
	}
	c, err := s.seal(ctx, merged)
	if err != nil {
		return err
	}
	batch, err := s.metadata(p, merged)
	if err != nil {
		return err
	}
	j := journal{Writes: writes, Batch: batch, Incoming: o.CheckpointID, Result: c.ID, Path: p.Path, GitBefore: local.Git, GitAfter: merged.Git}
	j.Artifacts, err = s.artifactMetadata(merged, batch)
	if err != nil {
		return err
	}
	j.ExpectedChats = map[string]session.PortableChat{}
	for _, chat := range batch.Chats {
		expected, err := s.store.ExportChat(merged.Chats[chat.SyncID].Harness, chat.ID)
		if err != nil {
			return err
		}
		if localEntry, ok := local.Entries["records/"+chat.SyncID]; ok {
			canonical := clone(expected)
			if err = portableAttachments(&canonical, p.Path); err != nil {
				return err
			}
			check, err := s.recordEntry(ctx, canonical, p.Path)
			if err != nil {
				return err
			}
			if !equal(check, localEntry) {
				return fmt.Errorf("recorded conversation changed while comparing; retry when work is saved")
			}
		} else if len(expected.Messages) != 0 || len(expected.Runs) != 0 {
			return fmt.Errorf("a destination chat ID already owns unrelated history")
		}
		j.ExpectedChats[chat.ID] = expected
	}
	// Preflight every selected recorded history before changing any user file.
	imports, err := s.chatImports(j, merged)
	if err != nil {
		return err
	}
	if err = s.store.ValidateChatImports(imports); err != nil {
		return err
	}
	if err = s.reg.SyncPut("journal", o.ID, j); err != nil {
		return err
	}
	keepLock = true
	o.ResultCheckpointID, o.ResultProjectID = c.ID, p.ID
	if err = s.applyJournal(ctx, o); err != nil {
		return err
	}
	keepLock = false
	return nil
}

func (s *Service) metadata(p registry.Project, m Manifest) (registry.Import, error) {
	snap, err := s.reg.Snapshot(nil)
	if err != nil {
		return registry.Import{}, err
	}
	p.Name, p.IconPath = m.Name, m.IconPath
	p.SyncID = m.ProjectID
	batch := registry.Import{Projects: []registry.Project{p}, Agents: []registry.Agent{}, Chats: []registry.Chat{}}
	profiles := map[string]registry.Agent{}
	for _, a := range snap.Agents {
		profiles[a.ID] = a
	}
	chats := map[string]registry.Chat{}
	for _, c := range snap.Chats {
		if c.ProjectID == p.ID {
			id := c.SyncID
			if id == "" {
				id = registry.SyncKey(snap.WorkspaceID, "chat", c.ID)
			}
			chats[id] = c
		}
	}
	ids := make([]string, 0, len(m.Chats))
	for id := range m.Chats {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		c := m.Chats[id]
		row, exists := chats[id]
		profile, found := profiles[row.AgentID]
		if !found {
			for _, a := range snap.Agents {
				if a.AgentType == c.Harness && a.Name == c.ProfileName {
					profile = a
					found = true
					break
				}
			}
		}
		if !found {
			profile = registry.Agent{Record: registry.Record{ID: "sync-" + registry.SyncKey(snap.WorkspaceID, "profile", c.Harness, c.ProfileName)[:32], WorkspaceID: snap.WorkspaceID, CreatedAt: now()}, Name: c.ProfileName, AgentType: c.Harness}
			profiles[profile.ID] = profile
			batch.Agents = append(batch.Agents, profile)
		}
		if !exists {
			row = registry.Chat{Record: registry.Record{ID: "sync-" + registry.SyncKey(snap.WorkspaceID, "chat-replica", id)[:32], WorkspaceID: snap.WorkspaceID, CreatedAt: c.CreatedAt}, ProjectID: p.ID, AgentID: profile.ID}
		}
		row.SyncID, row.Title, row.NativeTitle, row.TitleIsUserSet, row.UpdatedAt, row.SessionID = id, c.Title, c.NativeTitle, c.TitleIsUserSet, c.UpdatedAt, c.SessionID
		batch.Chats = append(batch.Chats, row)
	}
	return batch, nil
}

func (s *Service) planWrites(ctx context.Context, cwd string, local, merged Manifest) ([]Write, []Conflict, error) {
	paths := map[string]string{}
	resources := map[string]*Resource{}
	for _, m := range []Manifest{local, merged} {
		for _, chat := range m.Chats {
			for _, name := range chat.NativeFiles {
				dataPrefix := "data/" + chat.Harness + "/"
				if strings.HasPrefix(name, dataPrefix) {
					resources[name] = &Resource{Harness: chat.Harness, SessionID: chat.SessionID, Name: strings.TrimPrefix(name, dataPrefix)}
					paths[name] = name
					continue
				}
				prefix := "native/" + chat.Harness + "/"
				path, err := s.porters[chat.Harness].ImportSessionPath(cwd, chat.SessionID, strings.TrimPrefix(name, prefix))
				if err != nil {
					return nil, nil, err
				}
				// Canonicalize parents, not the leaf: replacing a symlink must replace
				// the link itself, never write into whatever it points at.
				parent, err := workspacepath.Canonical(filepath.Dir(path))
				if err != nil {
					return nil, nil, err
				}
				paths[name] = filepath.Join(parent, filepath.Base(path))
			}
		}
	}
	keys := map[string]bool{}
	for key := range local.Entries {
		keys[key] = true
	}
	for key := range merged.Entries {
		keys[key] = true
	}
	writes := []Write{}
	conflicts := []Conflict{}
	for key := range keys {
		if strings.HasPrefix(key, "records/") {
			continue
		}
		before, after := entry(local.Entries, key), entry(merged.Entries, key)
		if equal(before, after) {
			continue
		}
		if before != nil && after != nil && before.Kind != after.Kind && (before.Kind == "directory" || after.Kind == "directory") {
			conflicts = append(conflicts, Conflict{key, "file", "A folder became a file, or a file became a folder. Resolve the type change before synchronizing; both copies are preserved."})
			continue
		}
		path := paths[key]
		if strings.HasPrefix(key, "files/") {
			path = filepath.Join(cwd, filepath.FromSlash(strings.TrimPrefix(key, "files/")))
		}
		if strings.HasPrefix(key, "artifacts/") {
			path = filepath.Join(s.reg.Directory(), filepath.FromSlash(key))
			parent, e := workspacepath.Canonical(filepath.Dir(path))
			if e != nil {
				return nil, nil, e
			}
			path = filepath.Join(parent, filepath.Base(path))
		}
		if path == "" {
			return nil, nil, invalid("native artifact has no owning conversation")
		}
		jsonLines := after != nil && after.JSONLines || before != nil && before.JSONLines
		var actual *Entry
		var err error
		resource := resources[key]
		if resource != nil {
			actual, err = s.readResource(ctx, *resource, cwd)
			if err == nil && after != nil {
				data, e := s.expanded(*after, cwd)
				if e != nil {
					return nil, nil, e
				}
				err = s.porters[resource.Harness].(agent.SessionDataPorter).ValidateSessionData(ctx, cwd, resource.SessionID, resource.Name, data)
			}
		} else {
			actual, err = s.capture(ctx, path, cwd, jsonLines)
		}
		if err != nil && !os.IsNotExist(err) {
			return nil, nil, err
		}
		if actual != nil && strings.HasPrefix(key, "native/") {
			actual.Mode = 0600
		}
		if !equal(actual, before) {
			if equal(actual, after) {
				continue
			}
			conflicts = append(conflicts, Conflict{key, "file", "An existing file or native conversation is owned outside this checkpoint. It has been kept."})
			continue
		}
		writes = append(writes, Write{Path: path, Before: before, After: after, CWD: cwd, Private: strings.HasPrefix(key, "native/"), Resource: resource})
	}
	// Delete deepest first; then create parents before children. Replacements
	// of a directory by a file are rejected if any local child still survives.
	sort.Slice(writes, func(i, j int) bool {
		a, b := writes[i], writes[j]
		if a.After == nil && b.After != nil {
			return true
		}
		if a.After != nil && b.After == nil {
			return false
		}
		if a.After == nil {
			return len(a.Path) > len(b.Path)
		}
		if len(a.Path) != len(b.Path) {
			return len(a.Path) < len(b.Path)
		}
		return a.Path < b.Path
	})
	return writes, conflicts, nil
}

func (s *Service) chatImports(j journal, m Manifest) ([]session.ChatImport, error) {
	imports := make([]session.ChatImport, 0, len(j.Batch.Chats))
	for _, c := range j.Batch.Chats {
		e, ok := m.Entries["records/"+c.SyncID]
		if !ok {
			return nil, invalid("missing recorded conversation")
		}
		b, err := s.expanded(e, j.Path)
		if err != nil {
			return nil, err
		}
		var data session.PortableChat
		if err = json.Unmarshal(b, &data); err != nil {
			return nil, err
		}
		for _, run := range data.Runs {
			if run.Status == "running" {
				return nil, invalid("a live process cannot be transferred")
			}
		}
		item := session.ChatImport{ChatID: c.ID, Agent: m.Chats[c.SyncID].Harness, SessionID: c.SessionID, CWD: j.Path, Data: data}
		if expected, ok := j.ExpectedChats[c.ID]; ok {
			item.Expected = &expected
		}
		imports = append(imports, item)
	}
	return imports, nil
}
