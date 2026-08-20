// Package file is a notification channel that appends each notification as one JSON line (JSONL) to a
// local file named by NOTIFY_FILE. It is a self-registering plug-in for the notify registry: importing
// it for its init() is all it takes to add file delivery — no change to the send path. Zero security
// surface: a plain local append the daemon owner opted into via an env var, never reachable over HTTP.
package file

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/notify"
)

func init() { notify.Register(factory) }

// factory enables the file channel only when NOTIFY_FILE names a target path (else ok=false → skipped).
// The path is daemon-owner env config, not API-mutable, so it is read directly (the Store is ignored).
func factory(notify.Store) (notify.Notifier, bool) {
	path := strings.TrimSpace(os.Getenv("NOTIFY_FILE"))
	if path == "" {
		return nil, false
	}
	return &Notifier{Path: path}, true
}

// Notifier appends each notification to Path as one JSON object per line. Sends are serialized by a mutex
// so concurrent turns can't interleave a half-written line. The file is created if absent (0600).
type Notifier struct {
	Path string
	mu   sync.Mutex
}

var _ notify.Notifier = (*Notifier)(nil)

func (f *Notifier) Notify(_ context.Context, n agent.Notification) error {
	line, err := json.Marshal(n)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	fh, err := os.OpenFile(f.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer fh.Close()
	_, err = fh.Write(append(line, '\n'))
	return err
}
