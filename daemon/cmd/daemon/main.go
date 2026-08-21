// Command daemon is the mindwire daemon (mindwired): it hosts every registered agent
// adapter, drives turns, and relays a unified structured event stream to the client.
// The client speaks one generic protocol; the daemon's orchestrator dispatches each
// request to the selected agent's adapter. Design: https://mindwire.sh/docs/concepts/internals
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/api"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"

	// Agent adapters self-register on import. Add a blank import per agent.
	_ "github.com/oblien/mindwire/daemon/internal/agent/claude"
	_ "github.com/oblien/mindwire/daemon/internal/agent/codex"
	_ "github.com/oblien/mindwire/daemon/internal/agent/opencode"

	// Notification channels self-register with the notify registry on import (the webhook registers
	// from within the notify package). Add a blank import per pluggable channel.
	_ "github.com/oblien/mindwire/daemon/internal/notify/exec"
	_ "github.com/oblien/mindwire/daemon/internal/notify/file"
)

func main() {
	for _, a := range os.Args[1:] {
		if a == "--print-catalog" {
			printCatalog()
			return
		}
	}

	addr := env("ADDR", "127.0.0.1:8790") // loopback by default; set ADDR=0.0.0.0:8790 to expose on all interfaces
	statePath := env("STATE_PATH", "agent-state.json")
	defaultAgent := env("AGENT_TYPE", "claude-code") // default when a request omits ?agent=
	cwd := os.Getenv("AGENT_CWD")
	token := os.Getenv("DAEMON_TOKEN")

	if len(agent.All()) == 0 {
		log.Fatalf("no agent adapters registered")
	}

	store, err := session.Open(statePath)
	if err != nil {
		log.Fatalf("open state: %v", err)
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

	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"agent":"` + sup.Default() + `","version":"` + agent.Version + `"}`))
	})

	apiMux := http.NewServeMux()
	api.New(store, hub, sup).Register(apiMux)
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
	serve(srv)
}

// serve runs the HTTP server until SIGINT/SIGTERM, then drains connections gracefully.
// In-flight turns run on the supervisor's own background contexts, so they continue
// regardless; this just lets active HTTP requests (incl. SSE) finish before exit.
func serve(srv *http.Server) {
	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
	_ = enc.Encode(map[string]any{"version": agent.Version, "agents": agent.Catalog()})
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
