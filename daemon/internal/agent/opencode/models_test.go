package opencode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// The connect → usable flow depends on Mindwire passing the connected-provider
// environment into `opencode models`. The OpenCode CLI's provider-discovery rules
// are its own versioned behavior (recent versions no longer expose Google merely
// because GOOGLE_API_KEY is set), so test our adapter boundary with a stand-in
// executable rather than pinning a particular OpenCode release's output.
func TestModelsAreEnvScopedToConnectedProviders(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "opencode")
	script := "#!/bin/sh\n" +
		"if [ \"$GOOGLE_API_KEY\" = \"connected-key\" ]; then\n" +
		"  printf 'google/gemini-test\\n'\n" +
		"fi\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake opencode: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GOOGLE_API_KEY", "")
	resetModelListCache(t)

	// Distinct env sets use distinct cache keys, so this exercises two shell-outs
	// and confirms connected credentials are overlaid onto the child process.
	bare := providerIDs(adapterModels(t, nil))
	withGoogle := providerIDs(adapterModels(t, map[string]string{"GOOGLE_API_KEY": "connected-key"}))

	if bare["google"] {
		t.Fatalf("bare model listing unexpectedly included google: %v", keys(bare))
	}
	if !withGoogle["google"] {
		t.Fatalf("connected provider environment was not passed to opencode models.\nbare providers=%v\nwith-google providers=%v",
			keys(bare), keys(withGoogle))
	}
}

func TestConfiguredModelCatalogProviders(t *testing.T) {
	a := adapter{}
	if got := a.ConfiguredModelCatalogProviders(mapStore{}); len(got) != 0 {
		t.Fatalf("no connected providers = %v, want empty", got)
	}

	store := mapStore{
		agent.ProviderEnvValueKey("google", "GOOGLE_API_KEY"):            "google-key",
		agent.ProviderEnvValueKey("amazon-bedrock", "AWS_ACCESS_KEY_ID"): "access",
		ckProvider: "openai",
		ckAPIKey:   "openai-key",
	}
	got := a.ConfiguredModelCatalogProviders(store)
	want := []string{"amazon-bedrock", "google", "openai"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ConfiguredModelCatalogProviders = %v, want %v", got, want)
	}
}

func resetModelListCache(t *testing.T) {
	t.Helper()
	modelListMu.Lock()
	modelListCache = map[string]modelListEntry{}
	modelListMu.Unlock()
	t.Cleanup(func() {
		modelListMu.Lock()
		modelListCache = map[string]modelListEntry{}
		modelListMu.Unlock()
	})
}

func adapterModels(t *testing.T, env map[string]string) []string {
	t.Helper()
	models, err := adapter{}.Models(env)
	if err != nil {
		t.Fatalf("Models(%v): %v", env, err)
	}
	ids := make([]string, 0, len(models))
	for _, m := range models {
		ids = append(ids, m.ID)
	}
	return ids
}

// providerIDs reduces a set of `provider/model` ids to the set of providers present.
func providerIDs(ids []string) map[string]bool {
	set := map[string]bool{}
	for _, id := range ids {
		if p, _, ok := strings.Cut(id, "/"); ok {
			set[p] = true
		}
	}
	return set
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
