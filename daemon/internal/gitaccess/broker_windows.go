//go:build windows

package gitaccess

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
)

// Windows retains a loopback broker. Its random bearer key travels only in the
// child's environment (reading that environment requires process access), never
// in command-line arguments, URLs, or repository configuration.
func listenBroker() (net.Listener, string, func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", nil, err
	}
	return listener, "http://" + listener.Addr().String(), func() {}, nil
}

func brokerClient(endpoint string) (*http.Client, string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" ||
		u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, "", fmt.Errorf("invalid credential source")
	}
	return &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: helperTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, endpoint, nil
}
