package opencode

import (
	"os/exec"
	"strings"
	"testing"
)

// The whole "connect a provider → its models become usable" flow rests on one fact: `opencode models`
// only lists a provider's models when that provider's API-key env var is present in the run env. This
// test pins that behavior at the adapter boundary — enumerating with a provider env var must surface that
// provider's models, and enumerating without it must not. If opencode ever makes its listing static
// (env-insensitive) this breaks loudly, because the console's Models page would then depend on a
// mechanism that no longer works.
//
// Skips when the opencode binary isn't installed (CI without it): the assertion is meaningless without a
// real enumeration.
func TestModelsAreEnvScopedToConnectedProviders(t *testing.T) {
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode binary not installed; env-scoped model listing is an integration behavior")
	}

	// Distinct env sets → distinct cache keys, so this exercises two real shell-outs (no cross-talk).
	bare := providerIDs(adapterModels(t, nil))
	// A dummy value is enough — opencode gates the LISTING on the env var's presence, not its validity
	// (so this test needs no real credential and makes no authenticated call).
	withGoogle := providerIDs(adapterModels(t, map[string]string{"GOOGLE_API_KEY": "dummy-not-a-real-key"}))

	if bare["google"] {
		t.Skip("this opencode install already lists google without a key (ambient GOOGLE_API_KEY?); can't isolate the effect")
	}
	if !withGoogle["google"] {
		t.Fatalf("connecting google (GOOGLE_API_KEY in run env) did not surface any google/* models — the connect→usable flow is broken.\nbare providers=%v\nwith-google providers=%v",
			keys(bare), keys(withGoogle))
	}
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
