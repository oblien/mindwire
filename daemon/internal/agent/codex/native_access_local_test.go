package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// The installed CLI executes real shell tools against a local model fixture.
// This verifies PATH, user identity, environment and network on start AND resume,
// without inference, real credentials, or changes to the user's Codex home.
// Also runnable as root in Linux to catch accidental nested sandbox requirements.
func TestLocalNativeCommandAccess(t *testing.T) {
	if os.Getenv("CODEX_LOCAL") != "1" {
		t.Skip("set CODEX_LOCAL=1 to test native commands with the installed Codex")
	}
	for _, binary := range []string{"codex", "curl"} {
		if _, err := exec.LookPath(binary); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("MINDWIRE_ISOLATION", "direct")
	t.Setenv("MINDWIRE_TOOLCHAIN_DIR", t.TempDir())
	bin := filepath.Join(t.TempDir(), "native tools")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	fixture := "#!/bin/sh\nprintf 'native-user=%s native-env=%s\\n' \"$(id -u)\" \"$MINDWIRE_NATIVE_CHECK\"\n"
	if err := os.WriteFile(filepath.Join(bin, "mindwire-native-probe"), []byte(fixture), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprintf("app-server=%t", streaming), func(t *testing.T) {
			codexDir, cwd := t.TempDir(), t.TempDir()
			t.Setenv("CODEX_HOME", codexDir)
			var requests, probes atomic.Int32
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" && r.URL.Path == "/native-probe" {
					probes.Add(1)
					_, _ = fmt.Fprintln(w, "native-network-ok")
					return
				}
				if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/responses") {
					http.NotFound(w, r)
					return
				}
				var request map[string]any
				if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&request); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				index := int(requests.Add(1))
				if index%2 == 0 {
					// Inspect only this call's result, not the previous turn's history.
					callID := fmt.Sprintf("call_desktop_%d", index-1)
					output := localCommandOutput(request["input"], callID)
					for _, want := range []string{fmt.Sprintf("native-user=%d native-env=inherited", os.Geteuid()), "native-network-ok"} {
						if !strings.Contains(output, want) {
							t.Errorf("native command output missing %q: %s", want, output)
						}
					}
					writeLocalReply(w, index, "Native checks complete.")
					return
				}
				name, namespace := localCommandTool(request["tools"], "")
				command := "mindwire-native-probe && curl --fail --silent --show-error --connect-timeout 2 --max-time 4 " + agent.ShellQuote(server.URL+"/native-probe")
				// Verify the environment arriving at the harness. A tool can opt into
				// another login shell, whose own profile may intentionally replace PATH.
				args := map[string]any{"workdir": cwd, "login": false}
				switch name {
				case "exec_command":
					args["cmd"], args["yield_time_ms"] = command, 1000
				case "shell_command":
					args["command"], args["timeout_ms"] = command, 10000
				case "shell":
					args["command"], args["timeout_ms"] = []string{"bash", "-c", command}, 10000
				default:
					t.Error("native CLI did not advertise a command tool")
					writeLocalReply(w, index, "Missing shell.")
					return
				}
				writeLocalToolCall(w, index, namespace, name, args)
			}))
			defer server.Close()
			config := fmt.Sprintf("model = \"gpt-5.5\"\nmodel_provider = \"local\"\n[model_providers.local]\nname = \"Local native-access fixture\"\nbase_url = %q\nwire_api = \"responses\"\nrequires_openai_auth = false\nsupports_websockets = false\nrequest_max_retries = 0\nstream_max_retries = 0\n", server.URL)
			if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			in := agent.TurnInput{Message: "Run the native command check.", CWD: cwd,
				Config: map[string]string{keyModel: "gpt-5.5", keyEffort: "low"},
				Env:    map[string]string{"CODEX_API_KEY": "fixture-api-key", "OPENAI_BASE_URL": server.URL, "MINDWIRE_NATIVE_CHECK": "inherited"}}
			if streaming {
				in.Inbound = make(chan agent.Inbound)
			}
			for turn := 1; turn <= 2; turn++ {
				ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
				result, err := (adapter{}).RunStream(ctx, in, func(agent.Event) {})
				cancel()
				if err != nil || result.IsError || result.Text != "Native checks complete." {
					t.Fatalf("turn %d: %+v %v", turn, result, err)
				}
				if result.SessionID == "" || (in.SessionID != "" && result.SessionID != in.SessionID) {
					t.Fatal("native check did not resume the same conversation")
				}
				in.SessionID = result.SessionID
			}
			if requests.Load() != 4 || probes.Load() != 2 {
				t.Fatalf("start/resume requests=%d network probes=%d", requests.Load(), probes.Load())
			}
		})
	}
}

func localCommandTool(value any, namespace string) (string, string) {
	switch value := value.(type) {
	case map[string]any:
		name, _ := value["name"].(string)
		if value["type"] == "namespace" {
			namespace = name
		}
		if name == "exec_command" || name == "shell_command" || name == "shell" {
			return name, namespace
		}
		for _, child := range value {
			if name, scope := localCommandTool(child, namespace); name != "" {
				return name, scope
			}
		}
	case []any:
		for _, child := range value {
			if name, scope := localCommandTool(child, namespace); name != "" {
				return name, scope
			}
		}
	}
	return "", ""
}

func localCommandOutput(value any, callID string) string {
	items, _ := value.([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["type"] == "function_call_output" && item["call_id"] == callID {
			output, _ := json.Marshal(item["output"])
			return string(output)
		}
	}
	return ""
}
