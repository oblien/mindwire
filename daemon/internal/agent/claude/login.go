package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/proc"
	"github.com/oblien/mindwire/daemon/internal/toolchain"
)

const subscriptionMarker = "MINDWIRE_CLAUDE_SUBSCRIPTION"

func (m *authModule) beginLogin(ctx context.Context) agent.AuthState {
	m.mu.Lock()
	if m.login != nil && m.login.Active() {
		flow := m.login
		m.mu.Unlock()
		return flow.InitialState(ctx)
	}
	flow := agent.NewAuthFlow("login")
	m.login = flow
	done := make(chan struct{})
	m.loginDone = done
	go func() { defer close(done); m.runLogin(flow) }()
	m.mu.Unlock()
	return flow.InitialState(ctx)
}

func (m *authModule) cancelLogin() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.login != nil {
		m.login.Cancel()
	}
}

func (m *authModule) Logout(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	m.mu.Lock()
	if m.login != nil {
		m.login.Cancel()
	}
	done := m.loginDone
	m.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return errors.New("Claude sign-in is still closing; try signing out again")
		}
	}
	if method := m.store.Get("authMethod"); method != "" && method != "login" {
		return nil
	}
	command := subscriptionCommand("claude auth logout", map[string]string{subscriptionMarker: "native"})
	cmd := exec.CommandContext(ctx, "bash", "-lc", toolchain.Shell(command))
	proc.Group(cmd)
	cmd.Env = toolchain.Environment()
	if err := cmd.Run(); err != nil {
		return errors.New("Claude could not sign out; try again")
	}
	return nil
}

func (m *authModule) runLogin(flow *agent.AuthFlow) {
	ctx := flow.Context()
	env := map[string]string{subscriptionMarker: "native"}
	command := subscriptionCommand("claude auth login --claudeai", env)
	// The user opens the URL on their device, never a browser on the workspace.
	cmd := exec.CommandContext(ctx, "bash", "-lc", toolchain.Shell("export BROWSER=true; "+command))
	proc.Group(cmd)
	cmd.Env = toolchain.Environment()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		flow.Fail("Couldn't start Claude sign-in. Please try again.")
		return
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		flow.Fail("Couldn't start Claude sign-in. Please try again.")
		return
	}
	// Auth errors get a friendly state; raw output can include authorization codes.
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		flow.Fail("Couldn't start Claude sign-in. Check that Claude Code is installed.")
		return
	}
	flow.SetInput(func(code string) error {
		_, err := io.WriteString(stdin, code+"\n")
		return err
	})
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 4096), 64*1024)
	sc.Split(scanLoginOutput)
	for sc.Scan() {
		updateLoginOutput(flow, sc.Text())
	}
	scanErr := sc.Err()
	if scanErr != nil {
		flow.Fail("Couldn't read the Claude sign-in response. Please try again.")
	}
	err = cmd.Wait()
	if ctx.Err() != nil || err != nil || scanErr != nil {
		flow.Fail("Claude sign-in did not complete. Please start again and use the latest authorization code.")
		return
	}
	status, err := queryAuthStatus(ctx, env)
	if err != nil || !status.LoggedIn || status.methodLabel() != "login" {
		flow.Fail("Claude could not confirm your subscription sign-in. Please try again.")
		return
	}
	flow.Complete(func() error {
		// Older setup-token credentials must not override the new native account.
		if err := m.store.Set("oauthToken", ""); err != nil {
			return err
		}
		return m.store.Set("authMethod", "login")
	})
}

var loginURLPattern = regexp.MustCompile(`https://[^\s'"\x1b]+`)

const codePrompt = "Paste code here if prompted >"

// Claude's input prompt has no newline. A line-only scanner would never surface
// the field while the CLI is waiting for the user's code.
func scanLoginOutput(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, data[:i], nil
	}
	if i := bytes.Index(data, []byte(codePrompt)); i >= 0 {
		end := i + len(codePrompt)
		return end, data[:end], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func updateLoginOutput(flow *agent.AuthFlow, line string) {
	state := flow.State()
	if url := loginURLPattern.FindString(line); url != "" && state.URL == "" {
		state.URL, state.Status = url, "pending"
		state.Message = "Sign in with Claude in your browser, then return here."
		flow.Update(state)
	}
	if strings.Contains(line, codePrompt) {
		state = flow.State()
		state.Status = "needs_input"
		state.Message = "Paste the authorization code shown on the Claude sign-in page."
		state.Fields = []agent.Field{{Key: agent.AuthCodeKey, Label: "Authorization code", Type: agent.FieldSecret, Required: true}}
		flow.Update(state)
	}
}

// Explicit subscription selection also overrides old gateway/cloud values in
// native settings.json. All overrides are public/empty values; no token is put
// on argv or copied from the native credential cache.
func subscriptionOverrides(env map[string]string) map[string]any {
	values := map[string]any{
		"ANTHROPIC_API_KEY": "", "ANTHROPIC_AUTH_TOKEN": "", "ANTHROPIC_BASE_URL": "",
		"CLAUDE_CODE_USE_BEDROCK": "0", "CLAUDE_CODE_USE_VERTEX": "0", "CLAUDE_CODE_USE_FOUNDRY": "0",
	}
	if env[subscriptionMarker] == "native" {
		values["CLAUDE_CODE_OAUTH_TOKEN"] = ""
	}
	return values
}

func subscriptionShell(env map[string]string) string {
	prefix := "unset ANTHROPIC_API_KEY ANTHROPIC_AUTH_TOKEN ANTHROPIC_BASE_URL; export CLAUDE_CODE_USE_BEDROCK=0 CLAUDE_CODE_USE_VERTEX=0 CLAUDE_CODE_USE_FOUNDRY=0; "
	if env[subscriptionMarker] == "native" {
		prefix += "unset CLAUDE_CODE_OAUTH_TOKEN; "
	}
	return prefix
}

func subscriptionCommand(command string, env map[string]string) string {
	if env[subscriptionMarker] == "" {
		return command
	}
	settings, _ := json.Marshal(map[string]any{"env": subscriptionOverrides(env), "apiKeyHelper": ""})
	command = strings.Replace(command, "claude ", "claude --settings "+agent.ShellQuote(string(settings))+" ", 1)
	return subscriptionShell(env) + command
}

func subscriptionTurnSettings(options agent.TurnOptions, env map[string]string) (agent.TurnOptions, error) {
	if env[subscriptionMarker] == "" {
		return options, nil
	}
	settings := map[string]any{}
	if len(options.ClaudeSettings) > 0 {
		if err := json.Unmarshal(options.ClaudeSettings, &settings); err != nil {
			return options, err
		}
	}
	if settings == nil {
		settings = map[string]any{}
	}
	values, _ := settings["env"].(map[string]any)
	if values == nil {
		values = map[string]any{}
	}
	for key, value := range subscriptionOverrides(env) {
		values[key] = value
	}
	settings["env"], settings["apiKeyHelper"] = values, ""
	encoded, err := json.Marshal(settings)
	options.ClaudeSettings = encoded
	return options, err
}
