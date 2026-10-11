package gitaccess

import (
	"errors"
	"strings"
)

// AuthenticationRequired recognizes GitHub credential failures, including those
// emitted by commit hooks. Network failures and repository errors stay distinct.
func AuthenticationRequired(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrCredential) {
		return true
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "the workspace's github cli is not signed in") {
		return true
	}
	for _, line := range strings.Split(message, "\n") {
		if !strings.Contains(line, "github.com") {
			continue
		}
		for _, marker := range []string{
			"fatal: could not read username for ", "fatal: could not read password for ",
			"fatal: authentication failed for ", "permission denied (publickey)",
		} {
			if strings.Contains(line, marker) {
				return true
			}
		}
	}
	return false
}
