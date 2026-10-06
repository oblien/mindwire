package projectsync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/artifact"
	"github.com/oblien/mindwire/daemon/internal/session"
)

func TestUploadedFileRoundTripKeepsNativePathsAndSourceIntact(t *testing.T) {
	x, y := testService(t), testService(t)
	addProject(t, x, "x")
	xp, yp := nativeFixture{filepath.Join(x.root, "native")}, nativeFixture{filepath.Join(y.root, "native")}
	x.s.porters["fixture"], y.s.porters["fixture"] = xp, yp
	addChat(t, x, "chat", "one")
	store, err := artifact.New(x.reg)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("Durable requirements for the project")
	hash := sha256.Sum256(data)
	digest := hex.EncodeToString(hash[:])
	id := digest[:32]
	_, err = store.Upload("chat", id, artifact.UploadChunk{Name: "requirements.txt", Mime: "text/plain", SHA256: digest, Bytes: len(data), Data: data})
	if err != nil {
		t.Fatal(err)
	}
	refs, err := store.ResolveInputs("chat", []agent.Attachment{{ArtifactID: id}})
	if err != nil {
		t.Fatal(err)
	}
	text := "Inspect these requirements"
	native, _ := json.Marshal(map[string]any{"cwd": x.project, "text": text + agent.AttachmentReferenceText(refs), "path": refs[0].Path})
	putFile(t, filepath.Join(xp.root, "session/one/main.jsonl"), string(native)+"\n")
	if err := x.store.AddMessage(session.Message{ID: "file-input", ChatID: "chat", Role: "user", Text: text, CreatedAt: now(), Attachments: refs}); err != nil {
		t.Fatal(err)
	}
	exported := mustRun(t, x.s, "export", Request{ID: "export-file", ProjectID: "x"})
	relay(t, x.s, y.s, exported.ResultCheckpointID)
	dest := mustRun(t, y.s, "import", Request{ID: "import-file", CheckpointID: exported.ResultCheckpointID, Path: y.project})
	snap, err := y.reg.Snapshot(nil)
	if err != nil || len(snap.Chats) != 1 {
		t.Fatal(err)
	}
	messages := y.store.Messages(snap.Chats[0].ID)
	if len(messages) != 1 || len(messages[0].Attachments) != 1 {
		t.Fatal("attachment history missing")
	}
	attached := messages[0].Attachments[0]
	if attached.ArtifactID != id || attached.Path == refs[0].Path || !strings.HasPrefix(attached.Path, y.reg.Directory()) || len(attached.Data) != 0 {
		t.Fatalf("attachment not rebound: %+v", attached)
	}
	if bytes, err := os.ReadFile(attached.Path); err != nil || string(bytes) != string(data) {
		t.Fatal("file bytes missing", err)
	}
	if content := readFile(t, filepath.Join(yp.root, "session/one/main.jsonl")); !strings.Contains(content, attached.Path) || strings.Contains(content, refs[0].Path) {
		t.Fatal("native attachment paths not rebound", content)
	}
	back := mustRun(t, y.s, "export", Request{ID: "export-file-back", ProjectID: dest.ResultProjectID})
	relay(t, y.s, x.s, back.ResultCheckpointID)
	mustRun(t, x.s, "import", Request{ID: "import-file-back", CheckpointID: back.ResultCheckpointID})
	if readFile(t, refs[0].Path) != string(data) || len(x.store.Messages("chat")) != 1 {
		t.Fatal("round trip changed source or duplicated history")
	}
}
