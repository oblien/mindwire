package toolchain

import (
	"encoding/json"
	"strings"
	"testing"
)

func policyFixture() Catalog {
	c := Bundled()
	c.Revision++
	h := c.Harnesses["codex"]
	h.Releases = []Release{
		{Version: "1.0.0", Daemon: []DaemonRange{{Min: "0.1.15", MaxExclusive: "0.2.0"}}},
		{Version: "1.1.0", Daemon: []DaemonRange{{Min: "0.1.15", MaxExclusive: "0.3.0"}}},
		{Version: "2.0.0", Daemon: []DaemonRange{{Min: "0.2.0"}}},
	}
	c.Harnesses["codex"] = h
	return c
}

func TestCatalogApprovesNewCLIForOlderDaemonWithoutChangingItsVersion(t *testing.T) {
	c := policyFixture()
	old := c.Evaluate("codex", "0.1.16", "1.0.0")
	if old.Compatibility != "supported" || old.RecommendedVersion != "1.1.0" || !old.UpdateAvailable || !old.RequiresDaemonUpdate || old.RequiredDaemonVersion != "0.2.0" {
		t.Fatalf("old daemon decision: %+v", old)
	}
	current := c.Evaluate("codex", "0.2.1", "1.1.0")
	if current.RecommendedVersion != "2.0.0" || current.RequiresDaemonUpdate {
		t.Fatalf("new daemon: %+v", current)
	}
	// The same 2.0.0 build is incompatible on the older daemon, not silently accepted
	// merely because its version is numerically greater than the installed baseline.
	if got := c.Evaluate("codex", "0.1.16", "2.0.0"); got.Compatibility != "incompatible" {
		t.Fatalf("%+v", got)
	}
	if got := c.Evaluate("codex", "0.1.16", "3.0.0"); got.Compatibility != "untested" || got.UpdateAvailable {
		t.Fatalf("unknown version: %+v", got)
	}
}

func TestKnownBrokenVersionsCannotBecomeUpdateTargets(t *testing.T) {
	c := policyFixture()
	h := c.Harnesses["codex"]
	h.Excluded = []Exclusion{{Min: "1.1.0", MaxExclusive: "1.1.1", Daemon: []DaemonRange{{Min: "0.1.15"}}, Reason: "Native input replies are broken in this release."}}
	c.Harnesses["codex"] = h
	s := c.Evaluate("codex", "0.1.16", "1.1.0")
	if s.Compatibility != "incompatible" || s.RecommendedVersion != "1.0.0" || s.UpdateAvailable || !strings.Contains(s.Message, "input replies") {
		t.Fatalf("%+v", s)
	}
}

func TestCatalogValidationRequiresExactVersionsAndExplicitDaemonSupport(t *testing.T) {
	for _, bad := range []string{"latest", "^1.0.0", "1.0.0; touch /tmp/x", "../1.0.0", "1.01.0"} {
		c := policyFixture()
		h := c.Harnesses["codex"]
		h.Releases[0].Version = bad
		c.Harnesses["codex"] = h
		data, _ := json.Marshal(c)
		if _, err := DecodeCatalog(data); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	c := policyFixture()
	h := c.Harnesses["codex"]
	h.Releases[0].Daemon = nil
	c.Harnesses["codex"] = h
	data, _ := json.Marshal(c)
	if _, err := DecodeCatalog(data); err == nil {
		t.Fatal("accepted absent compatibility")
	}
}

func TestVersionOrderingAndNativeBanners(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"0.155.0", "0.99.0", 1}, {"1.0.0", "1.0.0-rc.1", 1}, {"1.0.0-rc.2", "1.0.0-rc.10", -1},
		{"1.0.0+build.1", "1.0.0+build.2", 0}, {"1.0.0-alpha", "1.0.0-alpha.1", -1},
	} {
		if got := compare(tc.a, tc.b); got != tc.want {
			t.Fatalf("%s / %s: %d", tc.a, tc.b, got)
		}
	}
	for _, tc := range []struct{ raw, want string }{{"codex-cli 0.155.0", "0.155.0"}, {"2.1.246 (Claude Code)", "2.1.246"}, {"grok 1.0.5 (5115b46bc909)", "1.0.5"}} {
		if got := ParseVersion(tc.raw); got != tc.want {
			t.Fatalf("%q -> %q", tc.raw, got)
		}
	}
}
