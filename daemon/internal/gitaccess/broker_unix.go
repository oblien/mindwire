//go:build !windows

package gitaccess

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
)

// Directory permissions also protect platforms where socket mode bits alone
// do not restrict connects. A process belonging to another OS user cannot use
// the broker even if it learns a helper's socket path or lease identifier.
func listenBroker() (net.Listener, string, func(), error) {
	directory, err := os.MkdirTemp("", "mw-git-")
	if err != nil {
		return nil, "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(directory) }
	path := filepath.Join(directory, "broker.sock")
	listener, err := net.Listen("unix", path)
	if err == nil {
		err = os.Chmod(path, 0600)
	}
	if err != nil {
		if listener != nil {
			_ = listener.Close()
		}
		cleanup()
		return nil, "", nil, err
	}
	return listener, path, cleanup, nil
}

func brokerClient(endpoint string) (*http.Client, string, error) {
	if !filepath.IsAbs(endpoint) {
		return nil, "", fmt.Errorf("invalid credential source")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: helperTimeout}).DialContext(ctx, "unix", endpoint)
	}}
	return &http.Client{Transport: transport, Timeout: helperTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, "http://localhost", nil
}
