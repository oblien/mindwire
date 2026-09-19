package registry

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func TestNotificationMutesInheritByProfileAndSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { st.Close() }()
	batch := fixture()
	batch.Agents = append(batch.Agents, Agent{Record: Record{ID: "other-profile"}, Name: "Other Codex", AgentType: "codex"})
	batch.Chats = append(batch.Chats,
		Chat{Record: Record{ID: "sibling"}, AgentID: "agent", ProjectID: "project", Title: "Sibling"},
		Chat{Record: Record{ID: "other-chat"}, AgentID: "other-profile", ProjectID: "project", Title: "Other"})
	if err := st.Import(batch); err != nil {
		t.Fatal(err)
	}
	check := func(id string, want bool) {
		t.Helper()
		got, err := st.NotificationsMuted(id)
		if err != nil || got != want {
			t.Fatalf("NotificationsMuted(%q) = %v, %v; want %v", id, got, err, want)
		}
	}
	for _, id := range []string{"chat", "sibling", "other-chat", "legacy-unregistered"} {
		check(id, false)
	}
	set := func(kind, id string, muted bool) {
		t.Helper()
		var value any
		var revision int64
		s := snapshot(t, st, nil)
		if kind == "agents" {
			for _, a := range s.Agents {
				if a.ID == id {
					a.NotificationsMuted = &muted
					value, revision = a, a.Revision
				}
			}
		} else {
			for _, c := range s.Chats {
				if c.ID == id {
					c.NotificationsMuted = &muted
					value, revision = c, c.Revision
				}
			}
		}
		data, _ := json.Marshal(value)
		if err := st.Put(kind, id, data, &revision); err != nil {
			t.Fatal(err)
		}
	}
	set("chats", "chat", true)
	check("chat", true)
	check("sibling", false)
	set("agents", "agent", true)
	set("chats", "chat", false)
	check("chat", true) // A chat cannot opt out of its parent's mute.
	check("sibling", true)
	check("other-chat", false) // The same harness under another profile remains independent.
	if err := st.Put("chats", "future", []byte(`{"agentId":"agent","projectId":"project","title":"Future"}`), nil); err != nil {
		t.Fatal(err)
	}
	check("future", true)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	check("chat", true)
	set("agents", "agent", false)
	check("chat", false)
	check("future", false)
	chat, _, _, err := st.ChatContext("chat")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Delete("chats", "chat", &chat.Revision, false); err != nil {
		t.Fatal(err)
	}
	check("chat", true) // Late notifications cannot resurrect a removed chat.
}

func TestOlderMetadataClientsCannotEraseNotificationPreferences(t *testing.T) {
	for _, kind := range []string{"agents", "chats"} {
		t.Run(kind, func(t *testing.T) {
			st := openTest(t)
			batch := fixture()
			muted := true
			batch.Agents[0].NotificationsMuted = &muted
			batch.Chats[0].NotificationsMuted = &muted
			if err := st.Import(batch); err != nil {
				t.Fatal(err)
			}
			id, renameKey := "agent", "name"
			var original any = snapshot(t, st, nil).Agents[0]
			if kind == "chats" {
				id, renameKey, original = "chat", "title", snapshot(t, st, nil).Chats[0]
			}
			data, _ := json.Marshal(original)
			var oldClient map[string]any
			_ = json.Unmarshal(data, &oldClient)
			delete(oldClient, "notificationsMuted")
			data, _ = json.Marshal(oldClient)
			if err := st.Put(kind, id, data, nil); err != nil {
				t.Fatal("unchanged retry from an older client should be idempotent:", err)
			}
			if snapshot(t, st, nil).Revision != 1 {
				t.Fatal("a missing optional field changed the record")
			}
			oldClient[renameKey] = "Renamed on an older device"
			oldClient["notificationsMuted"] = nil // Null also means preserve.
			data, _ = json.Marshal(oldClient)
			revision := int64(1)
			if err := st.Put(kind, id, data, &revision); err != nil {
				t.Fatal(err)
			}
			current := snapshot(t, st, nil)
			if !*current.Agents[0].NotificationsMuted || !*current.Chats[0].NotificationsMuted {
				t.Fatal("an unrelated rename erased a mute")
			}
			oldClient["notificationsMuted"] = false
			data, _ = json.Marshal(oldClient)
			if err := st.Put(kind, id, data, &revision); !errors.Is(err, ErrConflict) {
				t.Fatalf("a stale client cleared a mute: %v", err)
			}
			revision = current.Revision
			if err := st.Put(kind, id, data, &revision); err != nil {
				t.Fatal(err)
			}
			current = snapshot(t, st, nil)
			value := current.Agents[0].NotificationsMuted
			if kind == "chats" {
				value = current.Chats[0].NotificationsMuted
			}
			if value == nil || *value {
				t.Fatal("explicit false did not clear the mute")
			}
		})
	}
}

func TestNotificationPreferenceReadFailureIsNotAnUnmutedDefault(t *testing.T) {
	st := openTest(t)
	st.Close()
	if _, err := st.NotificationsMuted("chat"); err == nil {
		t.Fatal("unavailable preferences must be reported to the sender")
	}
}
