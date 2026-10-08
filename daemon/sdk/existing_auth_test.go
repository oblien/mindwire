package mindwire

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeAuthSourceIsSharedByGoSDKAndDaemon(t *testing.T) {
	host, private := t.TempDir(), t.TempDir()
	t.Setenv("CODEX_HOME", private)
	config := []byte(`model_provider = "work"
model = "private-model"
[model_providers.work]
name = "Work connection"
base_url = "https://fixture.example.test/v1"
experimental_bearer_token = "fixture-private"
`)
	if err := os.WriteFile(filepath.Join(host, "config.toml"), config, 0600); err != nil {
		t.Fatal(err)
	}
	c, err := New(Options{Agent: "codex", CWD: private, StatePath: filepath.Join(private, "state.json"),
		AuthSource: &AuthSource{Home: host, Environment: map[string]string{"CODEX_HOME": host}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if !c.Health().NativeAuthSource {
		t.Fatal("missing native auth source capability")
	}
	methods, err := c.Auth.Methods()
	if err != nil {
		t.Fatal(err)
	}
	if len(methods) == 0 || methods[0].Existing == nil || methods[0].Existing.Name != "Work connection" || methods[0].Existing.Model != "private-model" {
		t.Fatal("Go SDK did not use the adapter's native source discovery")
	}
	if _, err := os.Stat(filepath.Join(private, "config.toml")); !os.IsNotExist(err) {
		t.Fatal("listing methods must remain metadata-only")
	}
	status, err := c.Auth.Status(context.Background())
	if err != nil || !status.Configured {
		t.Fatalf("first status should reuse the host connection without auth.begin: %+v %v", status, err)
	}
	if _, err := os.Stat(filepath.Join(private, "config.toml")); err != nil {
		t.Fatal("first-use connection was not installed in the private profile")
	}
	if source, err := os.ReadFile(filepath.Join(host, "config.toml")); err != nil || string(source) != string(config) {
		t.Fatal("automatic reuse changed the source configuration")
	}
}
