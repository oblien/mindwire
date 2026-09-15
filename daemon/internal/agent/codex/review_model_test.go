package codex

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const reviewModelFixture = `{"models":[
  {"slug":"chat-model","base_instructions":"Native instructions","context_window":123456,
   "supported_reasoning_levels":[{"effort":"low","description":"Fast"}],"future_capability":{"enabled":true}},
  {"slug":"codex-auto-review","base_instructions":"Review instructions","auto_review_model_override":"old-reviewer"}
],"catalog_version":"native-version"}`

func TestReviewCatalogPreservesNativeMetadata(t *testing.T) {
	data, err := reviewCatalog([]byte(reviewModelFixture), "chat-model")
	if err != nil {
		t.Fatal(err)
	}
	var before, after map[string]any
	_ = json.Unmarshal([]byte(reviewModelFixture), &before)
	if err := json.Unmarshal(data, &after); err != nil {
		t.Fatal(err)
	}
	for _, model := range after["models"].([]any) {
		entry := model.(map[string]any)
		if entry["auto_review_model_override"] != "chat-model" {
			t.Fatalf("reviewer would use a different deployment: %+v", entry)
		}
		delete(entry, "auto_review_model_override")
	}
	for _, model := range before["models"].([]any) {
		delete(model.(map[string]any), "auto_review_model_override")
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("native model behavior changed: before=%+v after=%+v", before, after)
	}
}

func TestReviewCatalogUnknownDeploymentUsesNativeFallback(t *testing.T) {
	data, err := reviewCatalog([]byte(reviewModelFixture), "my-foundry-deployment")
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Models  []map[string]any `json:"models"`
		Version string           `json:"catalog_version"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil || len(catalog.Models) != 1 || catalog.Version != "native-version" {
		t.Fatalf("stock reviewer still advertised for unknown deployment: %s (%v)", data, err)
	}
	if catalog.Models[0]["slug"] != "chat-model" || catalog.Models[0]["auto_review_model_override"] != "my-foundry-deployment" {
		t.Fatalf("lost chat metadata or routed review to another deployment: %s", data)
	}
	for _, malformed := range []string{`not json`, `{}`, `{"models":"invalid"}`, `{"models":[]}`} {
		if _, err := reviewCatalog([]byte(malformed), "chat-model"); err == nil {
			t.Fatalf("accepted malformed native catalog: %s", malformed)
		}
	}
}

// Validate the override against the installed binary, without making model requests.
func TestReviewCatalogLocalNativeCompatibility(t *testing.T) {
	if os.Getenv("CODEX_LOCAL") != "1" {
		t.Skip("set CODEX_LOCAL=1 to validate catalogs with the installed Codex CLI")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	native, err := exec.CommandContext(ctx, "codex", "debug", "models").Output()
	if err != nil {
		t.Fatal(err)
	}
	const deployment = "mindwire-opaque-deployment-test"
	data, err := reviewCatalog(native, deployment)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "models.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "codex", "debug", "models", "-c", "model_catalog_json="+tomlString(path))
	checked, err := cmd.Output()
	if err != nil {
		t.Fatalf("native CLI rejected the review model catalog: %v", err)
	}
	var catalog struct {
		Models []struct {
			Slug     string `json:"slug"`
			Reviewer string `json:"auto_review_model_override"`
		} `json:"models"`
	}
	if err := json.Unmarshal(checked, &catalog); err != nil || len(catalog.Models) == 0 {
		t.Fatalf("native CLI returned an unusable model catalog: %v", err)
	}
	for _, model := range catalog.Models {
		if model.Slug == "codex-auto-review" || model.Reviewer != deployment {
			t.Fatalf("native CLI lost the provider's reviewer choice: %+v", model)
		}
	}
}

func TestPrepareReviewModelScopesAndCleansTemporaryCatalog(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\n[ \"$*\" = 'debug models' ] || exit 91\ncat <<'CATALOG'\n"+reviewModelFixture+"\nCATALOG\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, tc := range []struct {
		name        string
		server      appServer
		config      string
		wantCatalog bool
	}{
		{name: "manual review", server: appServer{reviewer: "user", provider: "foundry"}},
		{name: "default OpenAI", server: appServer{reviewer: "auto_review"}},
		{name: "OpenAI API key", server: appServer{reviewer: "auto_review", provider: "mindwire-api", config: map[string]any{"model_providers": map[string]any{"mindwire-api": map[string]any{"base_url": "https://api.openai.com/v1"}}}}},
		{name: "custom API key", server: appServer{reviewer: "auto_review", model: "chat-model", provider: "mindwire-api", config: map[string]any{"model_providers": map[string]any{"mindwire-api": map[string]any{"base_url": "https://foundry.example/v1"}}}}, wantCatalog: true},
		{name: "native Foundry", server: appServer{reviewer: "auto_review"}, config: "model = \"chat-model\"\nmodel_provider = \"foundry\"\n[model_providers.foundry]\nbase_url = \"https://foundry.example/v1\"\n", wantCatalog: true},
		{name: "native reviewer default", config: "approvals_reviewer = \"auto_review\"\nmodel = \"chat-model\"\nmodel_provider = \"foundry\"\n", wantCatalog: true},
		{name: "user managed catalog", server: appServer{reviewer: "auto_review", provider: "foundry"}, config: "model_catalog_json = \"/user/models.json\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nativeDir := t.TempDir()
			t.Setenv("CODEX_HOME", nativeDir)
			configPath := filepath.Join(nativeDir, "config.toml")
			if err := os.WriteFile(configPath, []byte(tc.config), 0o600); err != nil {
				t.Fatal(err)
			}
			a := tc.server
			a.command, a.cwd = "codex app-server", t.TempDir()
			cleanup, err := a.prepareReviewModel(context.Background())
			t.Cleanup(cleanup)
			if err != nil {
				t.Fatal(err)
			}
			const flag = ` -c 'model_catalog_json="`
			_, value, hasCatalog := strings.Cut(a.command, flag)
			if hasCatalog != tc.wantCatalog {
				t.Fatalf("unexpected startup command: %s", a.command)
			}
			if hasCatalog {
				path := strings.TrimSuffix(value, `"'`)
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != 0o600 {
					t.Fatalf("catalog must exist with private permissions: %v, %v", info, err)
				}
				data, err := os.ReadFile(path)
				if err != nil || !strings.Contains(string(data), `"auto_review_model_override":"chat-model"`) {
					t.Fatalf("reviewer deployment not configured: %s (%v)", data, err)
				}
				cleanup()
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("temporary catalog was not removed: %v", err)
				}
			}
			data, _ := os.ReadFile(configPath)
			if string(data) != tc.config {
				t.Fatal("changed the user's native Codex configuration")
			}
		})
	}
}
