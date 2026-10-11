package gitaccess

import (
	"errors"
	"fmt"
	"testing"
)

func TestAuthenticationRequiredDistinguishesGitHubLoginFromOtherFailures(t *testing.T) {
	for _, text := range []string{
		"fatal: could not read Username for 'https://github.com': terminal prompts disabled",
		"fatal: could not read Password for 'https://user@github.com': terminal prompts disabled",
		"fatal: Authentication failed for 'https://github.com/octocat/app.git/'",
		"git@github.com: Permission denied (publickey).",
		"the workspace's GitHub CLI is not signed in",
	} {
		if !AuthenticationRequired(errors.New(text)) {
			t.Errorf("not recognized: %s", text)
		}
	}
	for _, text := range []string{
		"fatal: unable to access 'https://github.com/octocat/app.git/': Could not resolve host: github.com",
		"Host key verification failed.", "Author identity unknown", "fatal: Unable to create .git/index.lock: File exists",
		"fatal: could not read Username for 'https://gitlab.example': terminal prompts disabled",
	} {
		if AuthenticationRequired(errors.New(text)) {
			t.Errorf("unrelated failure requires login: %s", text)
		}
	}
	if AuthenticationRequired(nil) || !AuthenticationRequired(fmt.Errorf("setup: %w", ErrCredential)) {
		t.Fatal("typed credential errors lost their classification")
	}
}
