package opencode

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// TestResolveAttachments covers the three routes: an inline image → a `file` vision part with a base64
// data: URL and inferred mime; a non-image with an on-disk Path → a path reference in the message
// suffix (no part); a non-image with inline Data → a temp file that exists during the turn and is
// removed by cleanup. It also confirms the returned cleanup is always safe to defer.
func TestResolveAttachments(t *testing.T) {
	atts := []agent.Attachment{
		{Name: "shot.png", Data: []byte{0x89, 0x50, 0x4e, 0x47}},           // image by extension → file part
		{Name: "notes.txt", Path: "/tmp/does-not-need-to-exist/notes.txt"}, // non-image with a path → reference
		{Name: "data.bin", Data: []byte("blob")},                           // non-image inline → temp file + reference
	}

	parts, msgAppend, cleanup, err := resolveAttachments(atts)
	if err != nil {
		t.Fatalf("resolveAttachments: %v", err)
	}
	defer cleanup()

	// Exactly one image part, shaped as opencode's file part.
	if len(parts) != 1 {
		t.Fatalf("parts = %d, want 1 image file part", len(parts))
	}
	p := parts[0]
	if p["type"] != "file" || p["mime"] != "image/png" || p["filename"] != "shot.png" {
		t.Fatalf("image part = %+v, want type/file mime/image/png filename/shot.png", p)
	}
	url, _ := p["url"].(string)
	if !strings.HasPrefix(url, "data:image/png;base64,") || len(url) <= len("data:image/png;base64,") {
		t.Fatalf("image part url = %q, want a non-empty data:image/png;base64 URL", url)
	}

	// Both non-image files are referenced by path in the suffix; the txt keeps its declared path.
	if !strings.Contains(msgAppend, "Attached files:") {
		t.Fatalf("msgAppend missing the attachments block: %q", msgAppend)
	}
	if !strings.Contains(msgAppend, "/tmp/does-not-need-to-exist/notes.txt") {
		t.Fatalf("msgAppend missing the path-referenced file: %q", msgAppend)
	}

	// The inline non-image was materialized to a temp file that exists now and references it by path.
	var tmpPath string
	for _, line := range strings.Split(msgAppend, "\n") {
		if strings.Contains(line, "data.bin") {
			if i := strings.Index(line, "("); i >= 0 {
				tmpPath = strings.TrimSuffix(line[i+1:], ")")
			}
		}
	}
	if tmpPath == "" {
		t.Fatalf("inline non-image not referenced in msgAppend: %q", msgAppend)
	}
	if b, e := os.ReadFile(tmpPath); e != nil || string(b) != "blob" {
		t.Fatalf("temp file %q contents = %q err=%v, want 'blob'", tmpPath, b, e)
	}
	cleanup()
	if _, e := os.Stat(tmpPath); !os.IsNotExist(e) {
		t.Fatalf("cleanup did not remove temp file %q (stat err=%v)", tmpPath, e)
	}
}

// TestResolveAttachmentsEmpty: no attachments → no parts, no suffix, and a non-nil, safe cleanup.
func TestResolveAttachmentsEmpty(t *testing.T) {
	parts, msgAppend, cleanup, err := resolveAttachments(nil)
	if err != nil || len(parts) != 0 || msgAppend != "" {
		t.Fatalf("empty attachments = parts:%v msg:%q err:%v, want none", parts, msgAppend, err)
	}
	cleanup() // must not panic on the empty case
}

// TestPromptCarriesImageFilePart drives converse end-to-end and asserts the prompt_async body carries
// the text part followed by the image `file` part — the wire shape opencode accepts (verified HTTP 204
// against the live server). Reuses the scripted approval server (auto-approve) so the turn completes.
func TestPromptCarriesImageFilePart(t *testing.T) {
	sc := newScriptServer("approval")
	ts := httptest.NewServer(sc.handler())
	defer ts.Close()

	filePart := map[string]any{
		"type": "file", "mime": "image/png", "url": "data:image/png;base64,AAAA", "filename": "a.png",
	}
	col := &collector{}
	srv := server{message: "look at this", extraParts: []map[string]any{filePart}, interactive: false}
	if _, got := srv.converse(context.Background(), ts.URL, nil, col.emit); !got {
		t.Fatal("converse reported no result")
	}

	sc.mu.Lock()
	body := sc.promptBody
	sc.mu.Unlock()
	var pb struct {
		Parts []map[string]any `json:"parts"`
	}
	if json.Unmarshal(body, &pb) != nil {
		t.Fatalf("prompt body not JSON: %s", body)
	}
	if len(pb.Parts) != 2 {
		t.Fatalf("prompt parts = %d, want 2 (text + file); body=%s", len(pb.Parts), body)
	}
	if pb.Parts[0]["type"] != "text" || pb.Parts[0]["text"] != "look at this" {
		t.Errorf("part[0] = %+v, want the text part", pb.Parts[0])
	}
	if pb.Parts[1]["type"] != "file" || pb.Parts[1]["url"] != "data:image/png;base64,AAAA" {
		t.Errorf("part[1] = %+v, want the image file part", pb.Parts[1])
	}
}
