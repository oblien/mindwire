package artifact

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/session"
)

func uploadStore(t *testing.T) (*Store, *registry.Store) {
	t.Helper()
	db, err := registry.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	return s, db
}

func uploadRequest(data []byte) UploadChunk {
	hash := sha256.Sum256(data)
	return UploadChunk{Name: "requirements.txt", Mime: "text/plain", Bytes: len(data), SHA256: hex.EncodeToString(hash[:])}
}

func TestUploadRetriesRestartAndDurableNativeReference(t *testing.T) {
	s, db := uploadStore(t)
	data := bytes.Repeat([]byte("project requirements\n"), 50_000)
	req := uploadRequest(data)
	req.Data = data[:MaxUploadChunk]
	const id = "0123456789abcdef0123456789abcdef"
	first, err := s.Upload("chat", id, req)
	if err != nil || first.Complete || first.Offset != MaxUploadChunk {
		t.Fatalf("first: %+v %v", first, err)
	}
	if _, _, err := s.Reference("chat", id); !errors.Is(err, registry.ErrNotFound) {
		t.Fatal("incomplete file exposed", err)
	}
	// Reopening the service and replaying an unacknowledged chunk is safe.
	s, err = New(db)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state, err := s.Upload("chat", id, req)
			if err != nil || state.Offset != first.Offset {
				t.Errorf("retry: %+v %v", state, err)
			}
		}()
	}
	wg.Wait()
	for offset := first.Offset; offset < len(data); {
		req.Offset = offset
		req.Data = data[offset:min(len(data), offset+MaxUploadChunk)]
		state, err := s.Upload("chat", id, req)
		if err != nil {
			t.Fatal(err)
		}
		offset = state.Offset
		if state.Complete != (offset == len(data)) {
			t.Fatal("wrong completion")
		}
	}
	content, err := s.Get(id)
	if err != nil || !bytes.Equal(content.Data, data) {
		t.Fatal("uploaded data changed", err)
	}
	resolved, err := s.ResolveInputs("chat", []agent.Attachment{{ArtifactID: id}})
	if err != nil || len(resolved) != 1 || len(resolved[0].Data) != 0 || filepath.Ext(resolved[0].Path) != ".txt" {
		t.Fatalf("reference: %+v %v", resolved, err)
	}
	if actual, err := os.ReadFile(resolved[0].Path); err != nil || !bytes.Equal(actual, data) {
		t.Fatal("native file missing", err)
	}
	if _, err := s.ResolveInputs("other-chat", []agent.Attachment{{ArtifactID: id}}); !errors.Is(err, ErrUploadConflict) {
		t.Fatal("cross-chat reference accepted", err)
	}
	if err := s.DeleteChat("chat"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(resolved[0].Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("attachment survived chat deletion")
	}
}

func TestUploadRejectsConflictsAndVerifiesChecksumBeforePublication(t *testing.T) {
	s, _ := uploadStore(t)
	const id = "0123456789abcdef0123456789abcdef"
	data := []byte("attachment contents")
	req := uploadRequest(data)
	req.Data = data[:5]
	if _, err := s.Upload("chat", id, req); err != nil {
		t.Fatal(err)
	}
	conflict := req
	conflict.Data = []byte("wrong")
	if _, err := s.Upload("chat", id, conflict); !errors.Is(err, ErrUploadConflict) {
		t.Fatal("changed retry accepted", err)
	}
	conflict = req
	conflict.Name = "changed.txt"
	if _, err := s.Upload("chat", id, conflict); !errors.Is(err, ErrUploadConflict) {
		t.Fatal("changed name accepted", err)
	}
	conflict = req
	conflict.Offset = 7
	if _, err := s.Upload("chat", id, conflict); !errors.Is(err, ErrUploadConflict) {
		t.Fatal("gap accepted", err)
	}
	corrupt := req
	corrupt.Offset = 5
	corrupt.Data = bytes.Repeat([]byte("x"), len(data)-5)
	if _, err := s.Upload("chat", id, corrupt); !errors.Is(err, ErrUploadConflict) {
		t.Fatal("bad digest published", err)
	}
	if _, err := s.Get(id); !errors.Is(err, registry.ErrNotFound) {
		t.Fatal("corrupt file exposed", err)
	}
	req.Data = data
	if state, err := s.Upload("chat", id, req); err != nil || !state.Complete {
		t.Fatal("could not restart verified upload", err)
	}
	for _, name := range []string{"../secret", "a/b", "a\\b", "a\ncommand"} {
		bad := req
		bad.Name = name
		if _, err := s.Upload("chat", id, bad); !errors.Is(err, ErrUpload) {
			t.Fatal("unsafe name", name, err)
		}
	}
	bad := req
	bad.Bytes = MaxAttachmentBytes + 1
	if _, err := s.Upload("chat", id, bad); !errors.Is(err, ErrUpload) {
		t.Fatal("oversized file accepted", err)
	}
}

func TestEmptyFileCanBeAttached(t *testing.T) {
	s, _ := uploadStore(t)
	state, err := s.Upload("chat", "0123456789abcdef0123456789abcdef", uploadRequest(nil))
	if err != nil || !state.Complete || state.Bytes != 0 {
		t.Fatal(state, err)
	}
	if !agent.HasUserInput("", agent.TurnOptions{Attachments: []agent.Attachment{{ArtifactID: state.ID}}}) {
		t.Fatal("empty file-only turn rejected")
	}
}

func TestUploadRecoversFinalRenameBeforeRegistryCommit(t *testing.T) {
	s, db := uploadStore(t)
	const id = "0123456789abcdef0123456789abcdef"
	data := []byte("complete bytes persisted before the process stopped")
	req := uploadRequest(data)
	rec := Record{ID: id, ChatID: "chat", Kind: "attachment", Name: req.Name, Mime: req.Mime,
		Bytes: req.Bytes, SHA256: req.SHA256, CreatedAt: time.Now().UTC()}
	meta, _ := json.Marshal(rec)
	if err := os.WriteFile(filepath.Join(s.dir, id+".upload.json"), meta, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.recordPath(rec), data, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	req.Offset, req.Data = 10, data[10:]
	state, err := s.Upload("chat", id, req)
	if err != nil || !state.Complete || state.Offset != len(data) {
		t.Fatalf("recovery: %+v %v", state, err)
	}
	got, err := s.Get(id)
	if err != nil || !bytes.Equal(got.Data, data) {
		t.Fatal("completed attachment was lost", err)
	}
}

func TestForkKeepsItsAttachmentAfterDeletingSource(t *testing.T) {
	s, _ := uploadStore(t)
	const id = "0123456789abcdef0123456789abcdef"
	data := []byte("shared requirements")
	req := uploadRequest(data)
	req.Data = data
	if _, err := s.Upload("source", id, req); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(t.TempDir(), "sessions.json")
	st, err := session.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.AddMessage(session.Message{ID: "input", ChatID: "source", Role: "user", Text: "Read this",
		CreatedAt: "2026-10-06T08:00:00Z", Attachments: []agent.Attachment{{ArtifactID: id}}}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"fork-one", "fork-two"} {
		if err = st.ForkChat("source", name); err != nil {
			t.Fatal(err)
		}
	}
	// The metadata survives reopen, without copying image/file bytes into history.
	st, err = session.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	for index, chat := range []string{"source", "fork-one", "fork-two"} {
		if _, err = st.DeleteChat(chat); err != nil {
			t.Fatal(err)
		}
		if err = s.DeleteChat(chat, st.ArtifactOwners(chat)); err != nil {
			t.Fatal(err)
		}
		got, readErr := s.Get(id)
		if index < 2 && (readErr != nil || !bytes.Equal(got.Data, data)) {
			t.Fatalf("remaining fork lost its file after deleting %s: %v", chat, readErr)
		}
		if index == 2 && !errors.Is(readErr, registry.ErrNotFound) {
			t.Fatal("last owner did not release attachment", readErr)
		}
	}
}
