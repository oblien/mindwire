package nativeauth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/oblien/mindwire/packages/daemon/internal/agent"
	"github.com/oblien/mindwire/packages/daemon/internal/proc"
	"github.com/oblien/mindwire/packages/daemon/internal/toolchain"
)

var httpsURL = regexp.MustCompile(`https://[^\s<>"'\x1b]+`)
var deviceCode = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{5,31}$`)
var ansi = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// CaptureURL exposes only an allowlisted authorization URL, never raw process
// output. Optional callback relay is bound to this URL's exact loopback + state.
func CaptureURL(flow *agent.AuthFlow, line string, hosts ...string) {
	for _, raw := range httpsURL.FindAllString(line, -1) {
		u, err := url.Parse(strings.TrimRight(raw, ").,]"))
		if err != nil {
			continue
		}
		allowed := false
		for _, host := range hosts {
			allowed = allowed || u.Hostname() == host
		}
		if !allowed || u.User != nil {
			continue
		}
		state := flow.State()
		state.Status, state.URL = "pending", u.String()
		if state.Message == "" {
			state.Message = "Open the link to finish signing in."
		}
		if code := u.Query().Get("user_code"); deviceCode.MatchString(code) {
			state.Code = code
		}
		redirect, err := url.Parse(u.Query().Get("redirect_uri"))
		oauthState := u.Query().Get("state")
		if err == nil && redirect.Scheme == "http" && redirect.User == nil && redirect.Port() != "" && oauthState != "" && (redirect.Hostname() == "localhost" || redirect.Hostname() == "127.0.0.1" || redirect.Hostname() == "::1") {
			state.CallbackURL = redirect.String()
			flow.SetCallback(func(raw string) error {
				callback, err := url.Parse(raw)
				if err != nil || callback.Scheme != redirect.Scheme || callback.Host != redirect.Host || callback.Path != redirect.Path || callback.User != nil || callback.Fragment != "" || callback.Query().Get("state") != oauthState || len(callback.Query()["state"]) != 1 {
					return errors.New("callback does not match this sign-in")
				}
				ctx, cancel := context.WithTimeout(flow.Context(), 20*time.Second)
				defer cancel()
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, callback.String(), nil)
				if err != nil {
					return err
				}
				client := &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
				defer client.CloseIdleConnections()
				response, err := client.Do(req)
				if err != nil {
					return err
				}
				response.Body.Close()
				if response.StatusCode >= 400 {
					return errors.New("native sign-in callback failed")
				}
				return nil
			})
		}
		flow.Update(state)
		return
	}
}

func LoginCommand(flow *agent.AuthFlow, binary string, args []string, env map[string]string, hosts ...string) error {
	cmd := toolchain.CommandContext(flow.Context(), binary, args...)
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Env = append(cmd.Env, "NO_OPEN_BROWSER=1", "BROWSER=true")
	proc.Group(cmd)
	output := &loginOutput{flow: flow, hosts: hosts}
	cmd.Stdout, cmd.Stderr = output, output
	err := cmd.Run()
	output.flush()
	return err
}

// Native CLIs may print their authorization handoff to either stream. Retain
// only bounded lines and publish allowlisted URLs; raw diagnostics stay private.
type loginOutput struct {
	mu       sync.Mutex
	data     []byte
	flow     *agent.AuthFlow
	hosts    []string
	wantCode bool
}

func (w *loginOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, b := range p {
		if b == '\n' || b == '\r' {
			w.line()
			w.data = nil
		} else if len(w.data) < 64*1024 {
			w.data = append(w.data, b)
		}
	}
	return len(p), nil
}
func (w *loginOutput) flush() { w.mu.Lock(); defer w.mu.Unlock(); w.line(); w.data = nil }
func (w *loginOutput) line() {
	line := strings.TrimSpace(ansi.ReplaceAllString(string(w.data), ""))
	if w.wantCode && deviceCode.MatchString(line) {
		state := w.flow.State()
		state.Code = line
		w.flow.Update(state)
		w.wantCode = false
	}
	if line == "Then enter this code:" || line == "Confirm this code in your browser:" {
		w.wantCode = true
	}
	CaptureURL(w.flow, line, w.hosts...)
}
