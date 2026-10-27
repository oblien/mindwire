package acp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/oblien/mindwire/packages/daemon/internal/acp"
	"github.com/oblien/mindwire/packages/daemon/internal/agent"
	_ "github.com/oblien/mindwire/packages/daemon/internal/agent/antigravity"
	_ "github.com/oblien/mindwire/packages/daemon/internal/agent/cursor"
	"github.com/oblien/mindwire/packages/daemon/internal/toolchain"
)

// Uses previously downloaded official archives, with the compiled checksums.
// Every binary, credential directory and managed selection lives in t.TempDir.
// No login or model prompt is issued; this verifies installation + native ACP.
func TestOfficialACPDistributions(t *testing.T) {
	paths := map[string]string{"cursor": os.Getenv("MINDWIRE_CURSOR_ARCHIVE"), "antigravity": os.Getenv("MINDWIRE_ANTIGRAVITY_ARCHIVE")}
	if paths["cursor"] == "" || paths["antigravity"] == "" {
		t.Skip("set MINDWIRE_CURSOR_ARCHIVE and MINDWIRE_ANTIGRAVITY_ARCHIVE to official native archives")
	}
	root := t.TempDir()
	t.Setenv("MINDWIRE_TOOLCHAIN_DIR", root)
	t.Setenv("MINDWIRE_HARNESS_CATALOG_URL", "off")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cursor.tar.gz":
			http.ServeFile(w, r, paths["cursor"])
		case "/antigravity.zip":
			http.ServeFile(w, r, paths["antigravity"])
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	manager := toolchain.New(agent.Version)
	for _, registered := range agent.All() {
		a, ok := registered.(acp.Adapter)
		if !ok {
			continue
		}
		t.Run(a.ID(), func(t *testing.T) {
			spec := a.Toolchain()
			decision := toolchain.Bundled().Evaluate(a.ID(), agent.Version, "")
			platform := runtime.GOOS + "/" + runtime.GOARCH
			archive := spec.Archives[decision.RecommendedVersion][platform]
			if archive.SHA256 == "" {
				t.Fatal("smoke requires a digest-pinned archive for this platform")
			}
			if a.ID() == "cursor" {
				archive.URL = server.URL + "/cursor.tar.gz"
			} else {
				archive.URL = server.URL + "/antigravity.zip"
			}
			spec.Archives[decision.RecommendedVersion][platform] = archive
			os.MkdirAll(filepath.Join(root, spec.Binary), 0700)
			selection, _ := json.Marshal(map[string]string{"version": "0.0.0"})
			os.WriteFile(filepath.Join(root, spec.Binary, "selected.json"), selection, 0600)
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			defer cancel()
			version, err := manager.Install(ctx, spec, true)
			if err != nil {
				t.Fatal(err)
			}
			software := manager.Software(ctx, spec)
			if !software.Managed || software.Compatibility != "supported" {
				t.Fatalf("installed software %+v", software)
			}
			env := map[string]string{"CURSOR_CONFIG_DIR": t.TempDir(), "CURSOR_DATA_DIR": t.TempDir(), "AGENT_CLI_CREDENTIAL_STORE": "file", "CURSOR_API_KEY": "", "CURSOR_AUTH_TOKEN": "", "GEMINI_HOME": t.TempDir(), "AGY_ACP_FORCE_FILE_STORAGE": "1", "GEMINI_API_KEY": "", "GOOGLE_API_KEY": "", "BROWSER": "true"}
			process, err := acp.Start(ctx, a.Flavor, agent.TurnInput{CWD: t.TempDir(), Env: env})
			if err != nil {
				t.Fatal(err)
			}
			defer process.Close()
			init, err := acp.Initialize(ctx, process.Client)
			if err != nil {
				t.Fatal(err)
			}
			if init.ProtocolVersion != 1 || !init.AgentCapabilities.LoadSession || !init.AgentCapabilities.PromptCapabilities.Image || !init.AgentCapabilities.MCPCapabilities.HTTP {
				t.Fatalf("unexpected handshake %+v", init)
			}
			t.Logf("%s %s: verified managed install, helper executable and native ACP handshake", a.ID(), version)
		})
	}
}
