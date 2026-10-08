package agent_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/authtest"
)

func TestNativeAuthBootstrapIsConsumedBeforeChildProcesses(t *testing.T) {
	home := t.TempDir()
	raw, _ := json.Marshal(agent.AuthSource{Home: home, Reuse: "manual", Environment: map[string]string{"NAMED_KEY": "fixture-private"}})
	t.Setenv(agent.AuthSourceEnvironment, string(raw))
	source, err := agent.ConsumeAuthSource()
	if err != nil || source.Home != home || source.Reuse != "manual" || source.Environment["NAMED_KEY"] != "fixture-private" {
		t.Fatal("native auth source was not decoded")
	}
	if _, present := os.LookupEnv(agent.AuthSourceEnvironment); present {
		t.Fatal("source credentials could leak to native children")
	}
	t.Setenv(agent.AuthSourceEnvironment, `{"home":"relative","environment":{"SECRET":"must-not-log"}}`)
	if _, err := agent.ConsumeAuthSource(); err == nil || strings.Contains(err.Error(), "must-not-log") {
		t.Fatal("invalid source must fail without disclosing its contents")
	}
	if _, present := os.LookupEnv(agent.AuthSourceEnvironment); present {
		t.Fatal("invalid source was retained")
	}
	raw, _ = json.Marshal(agent.AuthSource{Home: home, Reuse: "unsupported"})
	t.Setenv(agent.AuthSourceEnvironment, string(raw))
	if _, err := agent.ConsumeAuthSource(); err == nil {
		t.Fatal("invalid reuse mode was accepted")
	}
}

func TestNativeAuthImportRollsBackPrivateFilesAndSettings(t *testing.T) {
	root := t.TempDir()
	old, added := filepath.Join(root, "a.json"), filepath.Join(root, "b.json")
	if err := os.WriteFile(old, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	store := authtest.NewStore(map[string]string{"authMethod": "previous", "model": "keep", "provider:unrelated:key": "keep-key"})
	err := agent.ImportNativeAuth(context.Background(), store, map[string][]byte{old: []byte("new"), added: []byte("new")},
		map[string]string{"authMethod": "login", "apiKey": "new-key"}, func(context.Context) bool { return false })
	if err == nil {
		t.Fatal("failed verification was accepted")
	}
	data, _ := os.ReadFile(old)
	if string(data) != "original" {
		t.Fatal("original connection was not restored")
	}
	if _, err := os.Stat(added); !os.IsNotExist(err) {
		t.Fatal("unverified credential file was retained")
	}
	if store.Get("authMethod") != "previous" || store.Get("apiKey") != "" || store.Get("model") != "keep" || store.Get("provider:unrelated:key") != "keep-key" {
		t.Fatal("connection fields were not rolled back independently")
	}
	store.FailKey = "apiKey"
	err = agent.ImportNativeAuth(context.Background(), store, map[string][]byte{old: []byte("new")}, map[string]string{"apiKey": "new-key"}, func(context.Context) bool { t.Fatal("verified after failed save"); return true })
	data, _ = os.ReadFile(old)
	if err == nil || string(data) != "original" {
		t.Fatal("failed save did not restore private file")
	}
}

func TestNativeAuthImportCreatesPrivateFilesAndHonorsCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile", "auth.json")
	store := authtest.NewStore(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := agent.ImportNativeAuth(ctx, store, map[string][]byte{path: []byte("fixture")}, nil, func(context.Context) bool { t.Fatal("verified canceled request"); return true })
	if err == nil {
		t.Fatal("canceled import succeeded")
	}
	err = agent.ImportNativeAuth(context.Background(), store, map[string][]byte{path: []byte("fixture")}, nil, func(context.Context) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("credential file is not private")
	}
	if _, err := agent.ReadNativeAuthFile(filepath.Dir(path)); err == nil {
		t.Fatal("directory accepted as a credential file")
	}
}
