package grok

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/oblien/mindwire/packages/daemon/internal/acp"
	"github.com/oblien/mindwire/packages/daemon/internal/agent"
)

// No account or model prompt: validates the actual installed command and handshake.
func TestInstalledGrokACP(t *testing.T) {
	if os.Getenv("MINDWIRE_GROK_NATIVE") != "1" {
		t.Skip("set MINDWIRE_GROK_NATIVE=1 to exercise the installed Grok CLI")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	f := flavor()
	if binary := os.Getenv("MINDWIRE_GROK_BINARY"); binary != "" {
		f.Binary = binary
	}
	process, err := acp.Start(ctx, f, agent.TurnInput{CWD: t.TempDir(), Config: map[string]string{keyPermission: "ask"}, Env: map[string]string{"GROK_HOME": t.TempDir(), "XAI_API_KEY": "", "GROK_CODE_XAI_API_KEY": "", "BROWSER": "true"}})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	init, err := acp.Initialize(ctx, process.Client)
	if err != nil {
		t.Fatal(err)
	}
	if init.ProtocolVersion != 1 || !init.AgentCapabilities.LoadSession || !init.AgentCapabilities.MCPCapabilities.HTTP {
		t.Fatalf("unexpected native capabilities: %+v", init)
	}
	t.Logf("Grok %s: supervised command and native ACP handshake verified", init.AgentInfo.Version)
}
