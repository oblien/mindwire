// Package toolchain owns harness compatibility and managed CLI installations. Clients consume
// its decisions; none of the installation commands or compatibility rules live in a mobile app.
package toolchain

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

const PolicyVersion = 1
const CatalogURL = "https://raw.githubusercontent.com/oblien/mindwire/main/daemon/internal/toolchain/catalog.json"

//go:embed catalog.json
var bundled []byte

type Catalog struct {
	SchemaVersion int                `json:"schemaVersion"`
	Revision      int                `json:"revision"`
	Harnesses     map[string]Harness `json:"harnesses"`
}

type Harness struct {
	Releases []Release `json:"releases"`
	// Explicit exclusions apply to installed versions too. Missing entries mean untested,
	// not incompatible, and never cause automatic replacement of a user's existing CLI.
	Excluded []Exclusion `json:"excluded,omitempty"`
}

type DaemonRange struct {
	Min          string `json:"min"`
	MaxExclusive string `json:"maxExclusive,omitempty"`
}

type Release struct {
	Version string        `json:"version"`
	Daemon  []DaemonRange `json:"daemon"`
}

type Exclusion struct {
	Min          string        `json:"min"`
	MaxExclusive string        `json:"maxExclusive"`
	Daemon       []DaemonRange `json:"daemon"`
	Reason       string        `json:"reason"`
}

func Bundled() Catalog {
	c, err := DecodeCatalog(bundled)
	if err != nil {
		panic("invalid bundled harness catalog: " + err.Error())
	}
	return c
}

func DecodeCatalog(data []byte) (Catalog, error) {
	var c Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return c, err
	}
	if c.SchemaVersion != PolicyVersion {
		return c, fmt.Errorf("unsupported harness catalog schema %d", c.SchemaVersion)
	}
	if c.Revision < 1 || len(c.Harnesses) == 0 {
		return c, fmt.Errorf("empty harness catalog")
	}
	for id, h := range c.Harnesses {
		if id == "" || strings.ContainsAny(id, "/\\ \t\n") {
			return c, fmt.Errorf("invalid harness id")
		}
		seen := map[string]bool{}
		for _, r := range h.Releases {
			if !validVersion(r.Version) || seen[r.Version] {
				return c, fmt.Errorf("%s: invalid or duplicate exact version %q", id, r.Version)
			}
			seen[r.Version] = true
			if err := validateRanges(r.Daemon); err != nil {
				return c, fmt.Errorf("%s %s: %w", id, r.Version, err)
			}
		}
		for _, x := range h.Excluded {
			if !validVersion(x.Min) || !validVersion(x.MaxExclusive) || compare(x.Min, x.MaxExclusive) >= 0 || strings.TrimSpace(x.Reason) == "" {
				return c, fmt.Errorf("%s: invalid excluded version range", id)
			}
			if err := validateRanges(x.Daemon); err != nil {
				return c, err
			}
		}
	}
	return c, nil
}

func validateRanges(ranges []DaemonRange) error {
	if len(ranges) == 0 {
		return fmt.Errorf("daemon compatibility must be explicit")
	}
	for _, r := range ranges {
		if !validVersion(r.Min) || (r.MaxExclusive != "" && (!validVersion(r.MaxExclusive) || compare(r.Min, r.MaxExclusive) >= 0)) {
			return fmt.Errorf("invalid daemon version range")
		}
	}
	return nil
}

func matches(version string, ranges []DaemonRange) bool {
	if !validVersion(version) {
		return false
	}
	for _, r := range ranges {
		if compare(version, r.Min) >= 0 && (r.MaxExclusive == "" || compare(version, r.MaxExclusive) < 0) {
			return true
		}
	}
	return false
}

// Software is the authoritative update/readiness decision exposed through HTTP and both SDKs.
type Software struct {
	PolicyVersion         int      `json:"policyVersion"`
	InstalledVersion      string   `json:"installedVersion"`
	RecommendedVersion    string   `json:"recommendedVersion,omitempty"`
	LatestVersion         string   `json:"latestVersion,omitempty"` // latest approved catalog entry, not npm latest
	Compatibility         string   `json:"compatibility"`           // supported | untested | incompatible | not_installed
	Managed               bool     `json:"managed"`
	UpdateAvailable       bool     `json:"updateAvailable"`
	RequiresDaemonUpdate  bool     `json:"requiresDaemonUpdate"`
	RequiredDaemonVersion string   `json:"requiredDaemonVersion,omitempty"`
	Message               string   `json:"message,omitempty"`
	CatalogRevision       int      `json:"catalogRevision"`
	CatalogSource         string   `json:"catalogSource"` // bundled | cached | remote
	CatalogStale          bool     `json:"catalogStale"`
	MissingDependencies   []string `json:"missingDependencies,omitempty"`
}

func (c Catalog) Evaluate(id, daemonVersion, installed string) Software {
	s := Software{PolicyVersion: PolicyVersion, InstalledVersion: installed, CatalogRevision: c.Revision, Compatibility: "untested"}
	if installed == "" {
		s.Compatibility = "not_installed"
	}
	h := c.Harnesses[id]
	excluded := func(v string) string {
		for _, x := range h.Excluded {
			if validVersion(v) && matches(daemonVersion, x.Daemon) && compare(v, x.Min) >= 0 && compare(v, x.MaxExclusive) < 0 {
				return x.Reason
			}
		}
		return ""
	}
	for _, r := range h.Releases {
		if excluded(r.Version) != "" {
			continue
		}
		if s.LatestVersion == "" || compare(r.Version, s.LatestVersion) > 0 {
			s.LatestVersion = r.Version
		}
		if matches(daemonVersion, r.Daemon) {
			if s.RecommendedVersion == "" || compare(r.Version, s.RecommendedVersion) > 0 {
				s.RecommendedVersion = r.Version
			}
			if installed == r.Version {
				s.Compatibility = "supported"
			}
		} else if installed == r.Version {
			s.Compatibility = "incompatible"
		}
	}
	if reason := excluded(installed); reason != "" {
		s.Compatibility = "incompatible"
		s.Message = reason
	}
	// A newer CLI can remain available to newer daemons while this daemon still offers its
	// own compatible update. Show the requirement separately; never replace the compatible target.
	for _, r := range h.Releases {
		if r.Version != s.LatestVersion || matches(daemonVersion, r.Daemon) {
			continue
		}
		for _, d := range r.Daemon {
			if compare(daemonVersion, d.Min) < 0 && (s.RequiredDaemonVersion == "" || compare(d.Min, s.RequiredDaemonVersion) < 0) {
				s.RequiredDaemonVersion = d.Min
			}
		}
	}
	s.RequiresDaemonUpdate = s.RequiredDaemonVersion != ""
	s.UpdateAvailable = s.RecommendedVersion != "" && (installed == "" || (validVersion(installed) && compare(s.RecommendedVersion, installed) > 0))
	if s.Message == "" {
		switch s.Compatibility {
		case "untested":
			s.Message = "This installed version has not been tested with this Mindwire service."
		case "incompatible":
			s.Message = "This installed version is incompatible with this Mindwire service. Update the service or use a supported CLI version."
		}
	}
	return s
}
