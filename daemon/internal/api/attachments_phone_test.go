package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Optional physical-iPhone driver for the same isolated native CLI fixture.
// The existing paired SSH tunnel forwards this loopback listener; no public port,
// personal chat, live daemon restart, or paid model request is involved.
func servePhoneAttachmentFixture(t *testing.T, handoff string, a *API, mux *http.ServeMux, cwd string,
	started <-chan struct{}, release chan struct{}, setFile func(string), requests func() string, fileMarker string) {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	token := hex.EncodeToString(key)
	done := make(chan struct{})
	var releaseOnce, doneOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	defer func() {
		if run, ok := a.store.LatestRun("phone-attachment-check"); ok {
			a.sup.Cancel(run.ID)
		}
		a.sup.Wait()
	}()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "fixture credential required"})
			return
		}
		switch r.URL.Path {
		case "/healthz":
			writeJSON(w, 200, map[string]any{"ok": true, "version": "attachment-test", "imageAttachmentsVersion": 1,
				"attachmentUploadVersion": 1, "turnRequestVersion": 1, "queuedInputVersion": 1})
			return
		case "/__fixture/state":
			ready := false
			select {
			case <-started:
				ready = true
			default:
			}
			captured := requests()
			writeJSON(w, 200, map[string]any{"started": ready, "image": strings.Contains(captured, "input_image"),
				"fileRead": strings.Contains(captured, fileMarker), "blocked": strings.Contains(captured, "MW_INPUT_BLOCK")})
			return
		case "/__fixture/release":
			releaseOnce.Do(func() { close(release) })
			writeJSON(w, 200, map[string]bool{"ok": true})
			return
		case "/__fixture/finish":
			writeJSON(w, 200, map[string]bool{"ok": true})
			doneOnce.Do(func() { close(done) })
			return
		case "/turns":
			body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err != nil {
				t.Error(err)
				return
			}
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(body))
			var req turnReq
			if json.Unmarshal(body, &req) == nil {
				for _, attachment := range req.Options.Attachments {
					if len(attachment.Data) != 0 {
						t.Error("phone embedded file bytes in its turn instead of uploading a reference")
					}
					rec, path, err := a.surfaces.Artifacts().Reference(req.ChatID, attachment.ArtifactID)
					if err == nil && rec.Name == "notes.txt" {
						setFile(path)
					}
				}
			}
		}
		mux.ServeHTTP(w, r)
	}))
	defer server.Close()
	_, rawPort, _ := net.SplitHostPort(server.Listener.Addr().String())
	port, _ := strconv.Atoi(rawPort)
	data, _ := json.Marshal(map[string]any{"port": port, "token": token, "cwd": cwd, "fileMarker": fileMarker})
	if err := os.WriteFile(handoff, data, 0600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(handoff)
	t.Log("Physical iPhone fixture ready on a private loopback listener")
	select {
	case <-done:
	case <-time.After(10 * time.Minute):
		t.Fatal("physical phone did not complete the attachment check")
	}
	captured := requests()
	if !strings.Contains(captured, "input_image") || !strings.Contains(captured, fileMarker) {
		t.Fatal("native CLI did not receive the phone's image and uploaded file")
	}
}

func lastNativeUser(body []byte) string {
	var req struct {
		Input []json.RawMessage `json:"input"`
	}
	_ = json.Unmarshal(body, &req)
	for i := len(req.Input) - 1; i >= 0; i-- {
		var item struct {
			Role string `json:"role"`
		}
		if json.Unmarshal(req.Input[i], &item) == nil && item.Role == "user" {
			return string(req.Input[i])
		}
	}
	return ""
}
