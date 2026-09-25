// Command daemon is the mindwire daemon (mindwired): it hosts every registered agent
// adapter, drives turns, and relays a unified structured event stream to the client.
// The client speaks one generic protocol; the daemon's orchestrator dispatches each
// request to the selected agent's adapter. Design: https://mindwire.sh/docs/concepts/internals
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/api"
	"github.com/oblien/mindwire/daemon/internal/computer"
	"github.com/oblien/mindwire/daemon/internal/gitaccess"
	"github.com/oblien/mindwire/daemon/internal/gitauthor"
	"github.com/oblien/mindwire/daemon/internal/gitops"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
	"github.com/oblien/mindwire/daemon/internal/workspaceexec"

	// Agent adapters self-register on import. Add a blank import per agent.
	_ "github.com/oblien/mindwire/daemon/internal/agent/claude"
	_ "github.com/oblien/mindwire/daemon/internal/agent/codex"
	_ "github.com/oblien/mindwire/daemon/internal/agent/grok"
	_ "github.com/oblien/mindwire/daemon/internal/agent/opencode"

	// Notification channels self-register with the notify registry on import (the webhook registers
	// from within the notify package). Add a blank import per pluggable channel.
	_ "github.com/oblien/mindwire/daemon/internal/notify/exec"
	_ "github.com/oblien/mindwire/daemon/internal/notify/file"
	"github.com/oblien/mindwire/daemon/internal/toolchain"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--git-credential" || os.Args[1] == "--git-ssh") {
		var err error
		if os.Args[1] == "--git-credential" {
			err = gitaccess.Helper(os.Args[2:], os.Stdin, os.Stdout)
		} else {
			err = gitaccess.SSHHelper(os.Args[2:], os.Stdin, os.Stdout, os.Stderr)
		}
		if err != nil {
			log.Print(err)
			os.Exit(1)
		}
		return
	}
	computerMode := false
	for _, a := range os.Args[1:] {
		if a == "--computer" {
			computerMode = true
		}
		if a == "--print-catalog" {
			printCatalog()
			return
		}
		if a == "--print-toolchains" {
			printToolchains()
			return
		}
	}

	addr := env("ADDR", "127.0.0.1:8790") // loopback by default; set ADDR=0.0.0.0:8790 to expose on all interfaces
	statePath := env("STATE_PATH", "agent-state.json")
	defaultAgent := env("AGENT_TYPE", "claude-code") // default when a request omits ?agent=
	cwd := os.Getenv("AGENT_CWD")
	token := os.Getenv("DAEMON_TOKEN")
	if token == "" {
		log.Fatal("DAEMON_TOKEN is required; generate one with: openssl rand -hex 32")
	}

	if len(agent.All()) == 0 {
		log.Fatalf("no agent adapters registered")
	}
	if computerMode {
		unlock, err := computer.Lock(filepath.Dir(statePath))
		if err != nil {
			log.Fatal(err)
		}
		defer unlock()
	}

	store, err := session.Open(statePath)
	if err != nil {
		log.Fatalf("open state: %v", err)
	}
	workspaceRegistry, err := registry.Open(env("WORKSPACE_DB_PATH", filepath.Join(filepath.Dir(statePath), "workspace.db")))
	if err != nil {
		log.Fatalf("open workspace registry: %v", err)
	}
	defer workspaceRegistry.Close()
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	// Authorized workspace connections can recover this credential on another device. Never
	// return it from the registry API, put it in snapshots, or rotate it when a client reconnects.
	tokenPath := filepath.Join(filepath.Dir(statePath), "daemon.token")
	if err := os.WriteFile(tokenPath+".tmp", []byte(token), 0600); err != nil {
		log.Fatalf("persist daemon credential: %v", err)
	}
	if err := os.Chmod(tokenPath+".tmp", 0600); err != nil {
		log.Fatalf("protect daemon credential: %v", err)
	}
	if err := os.Rename(tokenPath+".tmp", tokenPath); err != nil {
		log.Fatalf("persist daemon credential: %v", err)
	}
	// A restart abandons any in-flight turn (it ran on the old process's context). Mark such runs
	// errored so a client reattaching doesn't hang forever on a stream nothing will publish to.
	_ = store.ReconcileRunning("interrupted by daemon restart")

	hub := stream.New()

	// Notifications: the daemon is a provider-agnostic emitter that fans each notification out to every
	// ENABLED channel (notify.Fanout over notify.All). The built-in webhook channel POSTs to a URL the
	// client sets via PUT /notify/config (read LIVE on each send); NOTIFY_URL seeds that store target for
	// dev/self-host. Additional local channels self-register via blank import and enable themselves from
	// their own env: notify/file (NOTIFY_FILE → JSONL append) and notify/exec (NOTIFY_EXEC → local hook on
	// stdin). No channel configured → the fan-out is a silent no-op.
	if u := os.Getenv("NOTIFY_URL"); u != "" {
		if cur, _, _ := store.NotifyConfig(); cur == "" {
			_ = store.SetNotifyConfig(u, os.Getenv("NOTIFY_CHANNEL"), os.Getenv("NOTIFY_TOKEN"))
		}
	}
	notifier := notify.Fanout(notify.All(store))

	// The orchestrator hosts every adapter and supervises turns; the API is glue over it.
	sup := orchestrator.New(store, hub, notifier, cwd, defaultAgent)
	workspaceAPI := api.New(store, hub, sup, workspaceRegistry)
	if err := workspaceAPI.InitError(); err != nil {
		log.Fatalf("recover project operations: %v", err)
	}
	defer workspaceAPI.Close()

	root := http.NewServeMux()
	health := http.NewServeMux()
	computerVersion := 0
	if computerMode {
		computerVersion = computer.Version
	}
	health.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "agent": sup.Default(), "version": agent.Version, "workspaceMetadataVersion": registry.Version, "projectOperationsVersion": registry.ProjectOperationsVersion, "surfaceProtocolVersion": 1, "notificationPreferencesVersion": registry.NotificationPreferencesVersion, "gitAccessVersion": gitaccess.Version, "gitOperationsVersion": gitops.Version, "gitIdentityVersion": gitauthor.Version, "harnessPolicyVersion": toolchain.PolicyVersion, "serviceUpdateVersion": orchestrator.ServiceUpdateVersion, "workspaceIsolationVersion": agent.WorkspaceIsolationVersion, "workspaceIsolation": agent.WorkspaceIsolation(), "workspaceExecutionVersion": workspaceexec.Version, "terminalProtocolVersion": workspaceexec.TerminalVersion, "computerConnectionVersion": computerVersion})
	})
	root.Handle("/healthz", api.Auth(token, health))

	apiMux := http.NewServeMux()
	workspaceAPI.Register(apiMux)
	if computerMode {
		connection, err := computer.New(filepath.Dir(statePath), listener.Addr().String(), token, workspaceRegistry.Identity())
		if err != nil {
			log.Fatalf("computer connection: %v", err)
		}
		workspaceAPI.SetConnectionActivity(connection.PendingActivity)
		connection.Register(apiMux, workspaceAPI.ConnectionOperation)
		if err = connection.Start(env("COMPUTER_SSH_ADDR", "0.0.0.0:8791"), env("COMPUTER_WS_ADDR", "127.0.0.1:8792"), filepath.Dir(statePath)); err != nil {
			log.Fatalf("computer listener: %v", err)
		}
		defer connection.Close()
	}
	root.Handle("/", api.Auth(token, apiMux))

	// DEV_CORS=1 allows a cross-origin browser app (e.g. the preview app's Vite dev server) to
	// reach the daemon. Off by default; wraps everything so OPTIONS preflight is handled.
	handler := api.CORS(os.Getenv("DEV_CORS") == "1", root)

	srv := &http.Server{
		Addr:    addr,
		Handler: handler,
		// ReadHeaderTimeout guards against Slowloris; IdleTimeout reaps idle keep-alives.
		// ReadTimeout bounds a whole request read. WriteTimeout is intentionally 0: the SSE
		// endpoints stream for the life of a run and a write deadline would sever them.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	log.Printf("agent-daemon: default-agent=%s addr=%s state=%s", sup.Default(), addr, statePath)
	serve(srv, listener)
}

// serve runs the HTTP server until SIGINT/SIGTERM, then drains connections gracefully.
// In-flight turns run on the supervisor's own background contexts, so they continue
// regardless; this just lets active HTTP requests (incl. SSE) finish before exit.
func serve(srv *http.Server, listener net.Listener) {
	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		log.Fatalf("server: %v", err)
	case sig := <-stop:
		log.Printf("agent-daemon: %s — shutting down", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("agent-daemon: graceful shutdown: %v", err)
		}
	}
}

func printCatalog() {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{"version": agent.Version, "agents": agent.Catalog(), "harnessCompatibility": toolchain.Bundled(), "harnessPolicyVersion": toolchain.PolicyVersion})
}

// Image builds use the same package declarations and approved versions as runtime setup.
// This emits data only; release automation never duplicates a list of npm latest packages.
func printToolchains() {
	plan, err := bundledToolchains()
	if err != nil {
		log.Fatal(err)
	}
	_ = json.NewEncoder(os.Stdout).Encode(plan)
}

type toolchainPlan struct {
	Agent       string            `json:"agent"`
	Binary      string            `json:"binary"`
	Package     string            `json:"package"`
	Version     string            `json:"version"`
	VersionArgs []string          `json:"versionArgs"`
	Environment map[string]string `json:"environment,omitempty"`
}

func bundledToolchains() ([]toolchainPlan, error) {
	catalog := toolchain.Bundled()
	plan := []toolchainPlan{}
	for _, a := range agent.All() {
		mod, ok := a.(agent.ToolchainModule)
		if !ok {
			continue
		}
		spec := mod.Toolchain()
		decision := catalog.Evaluate(a.ID(), agent.Version, "")
		if decision.RecommendedVersion == "" {
			return nil, fmt.Errorf("no bundled %s release supports daemon %s; update the compatibility catalog before releasing", a.ID(), agent.Version)
		}
		plan = append(plan, toolchainPlan{Agent: a.ID(), Binary: spec.Binary, Package: spec.Package, Version: decision.RecommendedVersion, VersionArgs: spec.VersionArgs, Environment: spec.Environment})
	}
	return plan, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
