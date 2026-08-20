package setup

import (
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func names(steps []agent.Step) []string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.Name
	}
	return out
}

func indexOf(ss []string, v string) int {
	for i, s := range ss {
		if s == v {
			return i
		}
	}
	return -1
}

func TestResolveOrdersDependenciesFirst(t *testing.T) {
	cat := map[string]agent.Step{
		"node": {Name: "node"},
		"git":  {Name: "git"},
	}
	// cli requires node; node and git have no deps.
	out, err := resolve(cat, []agent.Step{
		{Name: "git"},
		{Name: "cli", Requires: []string{"node"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := names(out)
	if indexOf(got, "node") < 0 || indexOf(got, "cli") < 0 {
		t.Fatalf("missing tools: %v", got)
	}
	if indexOf(got, "node") > indexOf(got, "cli") {
		t.Errorf("node must come before cli: %v", got)
	}
}

func TestResolveDedups(t *testing.T) {
	cat := map[string]agent.Step{"git": {Name: "git"}}
	// git requested directly AND via a's Requires — must appear once.
	out, err := resolve(cat, []agent.Step{
		{Name: "git"},
		{Name: "a", Requires: []string{"git"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, n := range names(out) {
		if n == "git" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("git appeared %d times, want 1: %v", count, names(out))
	}
}

func TestResolveDetectsCycle(t *testing.T) {
	_, err := resolve(nil, []agent.Step{
		{Name: "a", Requires: []string{"b"}},
		{Name: "b", Requires: []string{"a"}},
	})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Errorf("expected a cycle error, got %v", err)
	}
}

func TestResolveUnknownDependency(t *testing.T) {
	_, err := resolve(nil, []agent.Step{{Name: "a", Requires: []string{"ghost"}}})
	if err == nil || !strings.Contains(err.Error(), "unknown toolchain dependency") {
		t.Errorf("expected an unknown-dependency error, got %v", err)
	}
}

func TestPlanIncludesBaseAndOrders(t *testing.T) {
	// A claude-like agent step requiring node; Plan must add git (base) and pull node from
	// the catalog, ordered git/node before the agent step.
	plan, err := Plan([]agent.Step{
		{Name: "Claude Code", Requires: []string{"node"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := names(plan)
	for _, want := range []string{"git", "node", "Claude Code"} {
		if indexOf(got, want) < 0 {
			t.Fatalf("plan missing %q: %v", want, got)
		}
	}
	if indexOf(got, "node") > indexOf(got, "Claude Code") {
		t.Errorf("node must precede Claude Code: %v", got)
	}
}
