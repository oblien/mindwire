package registry

import (
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
)

func TestInstalledAgentsPreserveProfilesChatsAndNativeMembership(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	muted := true
	if err := st.Import(Import{
		Agents:   []Agent{{Record: Record{ID: "existing"}, Name: "My coding agent", AgentType: "codex", NotificationsMuted: &muted}},
		Projects: []Project{{Record: Record{ID: "project"}, Name: "App", Path: t.TempDir()}},
		Chats:    []Chat{{Record: Record{ID: "chat"}, AgentID: "existing", ProjectID: "project", SessionID: "native-session"}},
	}); err != nil {
		t.Fatal(err)
	}
	observations := []AgentInstallation{{"codex", "Codex", "installed"}, {"claude-code", "Claude Code", "installed"}, {"absent", "Absent", "missing"}}
	if err := st.SyncAgentInstallations(observations); err != nil {
		t.Fatal(err)
	}
	full, _ := st.Snapshot(nil)
	if len(full.Agents) != 2 || len(full.Chats) != 1 || full.Chats[0].AgentID != "existing" || full.Chats[0].SessionID != "native-session" {
		t.Fatalf("discovery changed membership or added a missing harness: %+v", full)
	}
	profile, err := st.EnsureAgent(EnsureAgentRequest{AgentType: "codex", Name: "Do not overwrite"}, "Codex")
	if err != nil || profile.ID != "existing" || profile.Name != "My coding agent" || profile.NotificationsMuted == nil || !*profile.NotificationsMuted {
		t.Fatalf("setup did not reuse the existing profile: %+v %v", profile, err)
	}
	if err := st.SyncAgentInstallations(observations); err != nil {
		t.Fatal(err)
	}
	unchanged, _ := st.Snapshot(&full.Revision)
	if unchanged.Revision != full.Revision || len(unchanged.Agents) != 0 {
		t.Fatal("unchanged scans wrote revisions", unchanged)
	}
	observations[0].State = "missing"
	if err := st.SyncAgentInstallations(observations); err != nil {
		t.Fatal(err)
	}
	delta, _ := st.Snapshot(&full.Revision)
	if len(delta.Agents) != 1 || delta.Agents[0].ID != "existing" || delta.Agents[0].Installation != "missing" || len(delta.Deleted) != 0 {
		t.Fatalf("uninstall must update presence, not delete history: %+v", delta)
	}
	// Empty/failed observations preserve the last successful result.
	if err := st.SyncAgentInstallations(nil); err != nil {
		t.Fatal(err)
	}
	still, _ := st.Snapshot(nil)
	if still.Revision != delta.Revision || len(still.Chats) != 1 {
		t.Fatal("failed scan erased data")
	}
	observations[0].State = "installed"
	if err := st.SyncAgentInstallations(observations); err != nil {
		t.Fatal(err)
	}
	restored, _ := st.EnsureAgent(EnsureAgentRequest{AgentType: "codex"}, "Codex")
	if restored.ID != profile.ID || restored.Installation != "installed" {
		t.Fatal("reinstall duplicated the profile", restored)
	}
}

func TestAgentSetupIsAtomicAndInstallationIsReadOnly(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	var wg sync.WaitGroup
	results := make(chan Agent, 24)
	errors := make(chan error, 24)
	for range 24 {
		wg.Go(func() {
			profile, err := st.EnsureAgent(EnsureAgentRequest{AgentType: "codex", Name: "Coding"}, "Codex")
			results <- profile
			errors <- err
		})
	}
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	full, _ := st.Snapshot(nil)
	if len(full.Agents) != 1 || full.Revision != 1 {
		t.Fatal("concurrent setup created duplicate profiles", full)
	}
	for profile := range results {
		if profile.ID != full.Agents[0].ID {
			t.Fatal("setup returned different IDs")
		}
	}
	if err := st.SyncAgentInstallations([]AgentInstallation{{"codex", "Codex", "installed"}}); err != nil {
		t.Fatal(err)
	}
	full, _ = st.Snapshot(nil)
	profile := full.Agents[0]
	profile.Installation, profile.Name = "missing", "Renamed"
	data, _ := json.Marshal(profile)
	if err := st.Put("agents", profile.ID, data, &profile.Revision); err != nil {
		t.Fatal(err)
	}
	renamed, _ := st.EnsureAgent(EnsureAgentRequest{AgentType: "codex"}, "Codex")
	if renamed.Name != "Renamed" || renamed.Installation != "installed" {
		t.Fatal("client overwrote source-owned presence", renamed)
	}
	spoofed := Agent{Record: Record{ID: "spoofed"}, Name: "Other", AgentType: "other", Installation: "installed"}
	if err := st.Import(Import{Agents: []Agent{spoofed}}); err != nil {
		t.Fatal(err)
	}
	imported, _ := st.EnsureAgent(EnsureAgentRequest{AgentType: "other"}, "Other")
	if imported.Installation != "" {
		t.Fatal("legacy import attested to an executable on another workspace")
	}
}

func TestRemovedDiscoveredAgentDoesNotResurrectUntilSetup(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	observations := []AgentInstallation{{"codex", "Codex", "installed"}}
	if err := st.SyncAgentInstallations(observations); err != nil {
		t.Fatal(err)
	}
	full, _ := st.Snapshot(nil)
	p := full.Agents[0]
	if err := st.Delete("agents", p.ID, &p.Revision, false); err != nil {
		t.Fatal(err)
	}
	if err := st.SyncAgentInstallations(observations); err != nil {
		t.Fatal(err)
	}
	empty, _ := st.Snapshot(nil)
	if len(empty.Agents) != 0 {
		t.Fatal("discovery resurrected an explicit removal")
	}
	next, err := st.EnsureAgent(EnsureAgentRequest{AgentType: "codex"}, "Codex")
	if err != nil || next.ID == p.ID {
		t.Fatal("explicit setup reused a deleted identity", next, err)
	}
}
