package toolchain

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/proc"
)

func TestInstallerReportsActivityBeforeExitAndBoundsDiagnosticOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stages := make(chan string, 8)
	ctx = WithInstallProgress(ctx, func(stage string) { stages <- stage })
	out := &installOutput{ctx: ctx}
	release := filepath.Join(t.TempDir(), "release")
	cmd := exec.CommandContext(ctx, "bash", "-c", "printf 'npm http f'; printf 'etch GET 200 https://registry.npmjs.org/package\\n' >&2; "+
		"printf 'npm info run package@1.0 postinstall\\n'; while [ ! -f "+quote(release)+" ]; do sleep 0.01; done")
	proc.Group(cmd)
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() { cancel(); <-done })
	for _, want := range []string{"downloading", "configuring"} {
		select {
		case got := <-stages:
			if got != want {
				t.Fatalf("live activity: got %q want %q", got, want)
			}
		case <-ctx.Done():
			t.Fatal("installer activity was buffered until exit")
		}
	}
	select {
	case err := <-done:
		done <- err
		t.Fatalf("installer exited before release: %v", err)
	default:
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	err := <-done
	done <- err // cleanup also waits for the process
	if err != nil {
		t.Fatal(err)
	}
	_, _ = out.Write([]byte(strings.Repeat("x", 128*1024)))
	if len(out.tail) > 64*1024 || len(out.pending) > 4096 {
		t.Fatal("package-manager output grew without bound")
	}
}

func TestSetupReusesFreshCatalogAndExplicitUpdateRefreshesIt(t *testing.T) {
	m, spec := fixtureManager(t)
	m.install = fakeInstall
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		_ = json.NewEncoder(w).Encode(policyFixture())
	}))
	defer server.Close()
	m.catalog.url = server.URL
	m.catalog.fetchedAt = time.Now()
	if _, err := m.Install(context.Background(), spec, false); err != nil {
		t.Fatal(err)
	}
	if reads.Load() != 0 {
		t.Fatal("first-time setup re-fetched fresh compatibility metadata")
	}
	if _, err := m.Install(context.Background(), spec, true); err != nil {
		t.Fatal(err)
	}
	if reads.Load() != 1 {
		t.Fatal("explicit update did not refresh compatibility metadata")
	}
}
