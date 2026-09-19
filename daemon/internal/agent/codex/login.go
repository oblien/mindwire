package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/proc"
	"github.com/oblien/mindwire/daemon/internal/toolchain"
)

// Apply after shell startup, which may export credentials from an older API
// connection. The native account cache, including refresh, belongs to Codex.
const subscriptionShell = "unset CODEX_API_KEY OPENAI_API_KEY CODEX_ACCESS_TOKEN OPENAI_BASE_URL OPENAI_ORGANIZATION OPENAI_PROJECT; "

func (m *authModule) beginLogin(ctx context.Context) agent.AuthState {
	m.mu.Lock()
	if m.login != nil && m.login.Active() {
		flow := m.login
		m.mu.Unlock()
		return flow.InitialState(ctx)
	}
	flow := agent.NewAuthFlow("login")
	m.login = flow
	m.mu.Unlock()
	go m.runLogin(flow)
	return flow.InitialState(ctx)
}

func (m *authModule) cancelLogin() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.login != nil {
		m.login.Cancel()
	}
}

func (m *authModule) runLogin(flow *agent.AuthFlow) {
	ctx := flow.Context()
	client, err := startAuthRPC(ctx)
	if err != nil {
		flow.Fail("Couldn't start Codex sign-in. Check that Codex is installed and try again.")
		return
	}
	defer client.close()
	var login struct {
		Type            string `json:"type"`
		LoginID         string `json:"loginId"`
		VerificationURL string `json:"verificationUrl"`
		UserCode        string `json:"userCode"`
	}
	if err := client.call(ctx, "account/login/start", map[string]any{"type": "chatgptDeviceCode"}, &login); err != nil {
		flow.Fail("Couldn't start ChatGPT sign-in: " + err.Error())
		return
	}
	if login.Type != "chatgptDeviceCode" || login.LoginID == "" || login.VerificationURL == "" || login.UserCode == "" {
		flow.Fail("Codex didn't return a device code. Update Codex and try again.")
		return
	}
	client.loginID = login.LoginID
	flow.Update(agent.AuthState{Status: "pending", URL: login.VerificationURL, Code: login.UserCode,
		Message: "Open the sign-in page, enter this code, and sign in with ChatGPT. This page will update automatically."})
	for {
		frame, err := client.next(ctx)
		if err != nil {
			flow.Fail("ChatGPT sign-in did not complete. Please start again.")
			return
		}
		if frame.Method != "account/login/completed" {
			continue
		}
		var completed struct {
			LoginID string `json:"loginId"`
			Success bool   `json:"success"`
			Error   string `json:"error"`
		}
		if json.Unmarshal(frame.Params, &completed) != nil || completed.LoginID != login.LoginID {
			continue
		}
		if !completed.Success {
			flow.Fail(agent.FirstNonEmpty(completed.Error, "ChatGPT sign-in was not completed. Please try again."))
			return
		}
		account, err := client.account(ctx)
		if err != nil || account == nil || account.Type != "chatgpt" {
			flow.Fail("Codex could not confirm the ChatGPT account. Please try again.")
			return
		}
		client.loginID = "" // native login is complete; no pending grant to cancel
		flow.Complete(func() error { return m.store.Set(ckMethod, "login") })
		return
	}
}

type nativeAccount struct {
	Type     string `json:"type"`
	PlanType string `json:"planType"`
}

func nativeSubscriptionStatus(ctx context.Context) agent.AuthStatus {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client, err := startAuthRPC(ctx)
	if err != nil {
		return agent.AuthStatus{Detail: "Couldn't check the Codex account. Check that Codex is installed."}
	}
	defer client.close()
	account, err := client.account(ctx)
	if err != nil {
		return agent.AuthStatus{Detail: "Couldn't check the Codex account. Please try again."}
	}
	if account != nil && account.Type == "chatgpt" {
		return agent.AuthStatus{Configured: true, Method: "login", Detail: "Signed in with ChatGPT"}
	}
	return agent.AuthStatus{Detail: "Continue with ChatGPT or connect an API provider."}
}

// Auth uses the same structured native app-server protocol as chat, in a
// separate bounded process so starting/canceling sign-in cannot stop a turn.
// Only account metadata crosses this boundary; auth.json/tokens never do.
type authRPC struct {
	stdin   io.WriteCloser
	frames  chan rpcIn
	done    chan struct{}
	cancel  context.CancelFunc
	events  []rpcIn
	seq     int
	loginID string
}

func startAuthRPC(ctx context.Context) (*authRPC, error) {
	processCtx, cancel := context.WithTimeout(context.Background(), 11*time.Minute)
	cmd := exec.CommandContext(processCtx, "bash", "-lc", toolchain.Shell(subscriptionShell+"codex app-server -c 'model_provider=\"openai\"'"))
	proc.Group(cmd)
	cmd.Env = toolchain.Environment()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		cancel()
		return nil, err
	}
	// Native stderr may contain OAuth details. It is never an API error/log body.
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		cancel()
		return nil, err
	}
	c := &authRPC{stdin: stdin, frames: make(chan rpcIn, 32), done: make(chan struct{}), cancel: cancel}
	go func() {
		defer close(c.done)
		defer close(c.frames)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 4096), 1<<20)
		for sc.Scan() {
			var frame rpcIn
			if json.Unmarshal(sc.Bytes(), &frame) != nil {
				continue
			}
			select {
			case c.frames <- frame:
			case <-processCtx.Done():
				_ = cmd.Wait()
				return
			}
		}
		_ = cmd.Wait()
	}()
	initCtx, stop := context.WithTimeout(ctx, 15*time.Second)
	defer stop()
	err = c.call(initCtx, "initialize", map[string]any{
		"clientInfo":   map[string]any{"name": clientName, "version": agent.Version},
		"capabilities": map[string]any{"experimentalApi": true},
	}, nil)
	if err == nil {
		err = json.NewEncoder(stdin).Encode(rpcNotification{Method: "initialized", Params: map[string]any{}})
	}
	if err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

func (c *authRPC) call(ctx context.Context, method string, params any, result any) error {
	c.seq++
	id := strconv.Itoa(c.seq)
	if err := json.NewEncoder(c.stdin).Encode(rpcRequest{ID: json.RawMessage(id), Method: method, Params: params}); err != nil {
		return errors.New("Codex sign-in connection closed")
	}
	for {
		frame, err := c.read(ctx)
		if err != nil {
			return err
		}
		if string(frame.ID) != id {
			if strings.HasPrefix(frame.Method, "account/") && len(c.events) < 32 {
				c.events = append(c.events, frame)
			}
			continue
		}
		if frame.Error != nil {
			if frame.Error.Code == -32601 || strings.Contains(frame.Error.Message, "chatgptDeviceCode") {
				return errors.New("update Codex to use ChatGPT device-code sign-in")
			}
			return fmt.Errorf("%s", frame.Error.Message)
		}
		if result != nil {
			return json.Unmarshal(frame.Result, result)
		}
		return nil
	}
}

func (c *authRPC) account(ctx context.Context) (*nativeAccount, error) {
	var result struct {
		Account *nativeAccount `json:"account"`
	}
	err := c.call(ctx, "account/read", map[string]any{"refreshToken": false}, &result)
	return result.Account, err
}

func (c *authRPC) read(ctx context.Context) (rpcIn, error) {
	select {
	case frame, ok := <-c.frames:
		if !ok {
			return rpcIn{}, errors.New("Codex sign-in connection closed")
		}
		return frame, nil
	case <-ctx.Done():
		return rpcIn{}, ctx.Err()
	}
}

func (c *authRPC) next(ctx context.Context) (rpcIn, error) {
	if len(c.events) > 0 {
		frame := c.events[0]
		c.events = c.events[1:]
		return frame, nil
	}
	return c.read(ctx)
}

func (c *authRPC) close() {
	if c.loginID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = c.call(ctx, "account/login/cancel", map[string]any{"loginId": c.loginID}, nil)
		cancel()
	}
	_ = c.stdin.Close()
	c.cancel()
	<-c.done
}
