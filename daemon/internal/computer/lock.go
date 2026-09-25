package computer

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

// Lock prevents competing launches from replacing the computer's host identity
// or reconciling the same workspace state. OS locks release automatically on exit.
func Lock(directory string) (func(), error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	lock := flock.New(filepath.Join(directory, "computer.lock"))
	ok, err := lock.TryLock()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("Mindwire is already running for this computer")
	}
	return func() { _ = lock.Close() }, nil
}
