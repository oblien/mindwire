package toolchain

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/proc"
)

// Spec is compiled into an adapter. The remote catalog can approve versions, but cannot
// change a package, execute shell code, or redirect an install to a different registry.
type Spec struct {
	ID          string
	Name        string
	Binary      string
	Package     string
	VersionArgs []string
	Environment map[string]string
}

type Manager struct {
	root    string
	version string
	catalog *catalogCache
	mu      sync.Mutex
	probes  map[string]probe
	install func(context.Context, Spec, string, string) error
}

type probe struct {
	raw      string
	version  string
	selected string
	at       time.Time
}

func New(daemonVersion string) *Manager {
	url := os.Getenv("MINDWIRE_HARNESS_CATALOG_URL")
	if url == "" {
		url = CatalogURL
	}
	return newManager(Root(), daemonVersion, url)
}

func newManager(root, version, url string) *Manager {
	return &Manager{root: root, version: version, catalog: newCatalogCache(root, url), probes: map[string]probe{}, install: installNPM}
}

func (m *Manager) Software(ctx context.Context, spec Spec) Software {
	p := m.probe(ctx, spec)
	c, source, stale := m.catalog.snapshot()
	installed := p.version
	if installed == "" && p.raw != "" {
		installed = p.raw
	}
	s := c.Evaluate(spec.ID, m.version, installed)
	s.Managed = p.selected != ""
	s.CatalogSource, s.CatalogStale = source, stale
	if s.Managed && p.version != p.selected {
		s.Compatibility = "incompatible"
		s.Message = "The managed CLI no longer matches its selected version. Reinstall the supported version."
	}
	return s
}

func (m *Manager) RefreshSoftware(ctx context.Context, spec Spec, force bool) Software {
	_ = m.catalog.refresh(ctx, force)
	if force {
		m.invalidate(spec)
	}
	return m.Software(ctx, spec)
}

// Admission is an explicit action, unlike passive UI reads. Probe the actual binary so a
// user-driven external upgrade or a damaged managed install cannot reuse stale readiness.
// Compatibility metadata remains cached: sending a message never waits on the catalog host.
func (m *Manager) CheckAdmission(ctx context.Context, spec Spec) error {
	m.invalidate(spec)
	info := m.Software(ctx, spec)
	if info.Compatibility == "incompatible" {
		return fmt.Errorf("%s", info.Message)
	}
	return nil
}

func (m *Manager) Version(ctx context.Context, spec Spec) string { return m.probe(ctx, spec).raw }

func (m *Manager) Catalog() Catalog { c, _, _ := m.catalog.snapshot(); return c }

func (m *Manager) invalidate(spec Spec) { m.mu.Lock(); delete(m.probes, spec.Binary); m.mu.Unlock() }

func (m *Manager) probe(ctx context.Context, spec Spec) probe {
	selectedVersion := selected(m.root, spec.Binary)
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.probes[spec.Binary]; ok && p.selected == selectedVersion && time.Since(p.at) < 30*time.Second {
		return p
	}
	path := spec.Binary
	if selectedVersion != "" {
		path = versionBinary(m.root, spec.Binary, selectedVersion)
	}
	raw, _ := versionOutput(ctx, spec, path)
	p := probe{raw: raw, version: ParseVersion(raw), selected: selectedVersion, at: time.Now()}
	m.probes[spec.Binary] = p
	return p
}

func versionOutput(ctx context.Context, spec Spec, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var args []string
	args = append(args, quote(path))
	for _, arg := range spec.VersionArgs {
		args = append(args, quote(arg))
	}
	cmd := exec.CommandContext(ctx, "bash", "-lc", strings.Join(args, " "))
	cmd.Env = os.Environ()
	for k, v := range spec.Environment {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	proc.Group(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Check is a setup probe, not an update. An installed, untested external CLI stays installed.
// A known incompatible version is rejected before it can start another turn.
func (m *Manager) Check(ctx context.Context, spec Spec) (string, error) {
	s := m.Software(ctx, spec)
	if s.Compatibility == "incompatible" {
		return "", fmt.Errorf("%s", s.Message)
	}
	if s.InstalledVersion == "" {
		return "", fmt.Errorf("%s is not installed", spec.Name)
	}
	return s.InstalledVersion, nil
}

// Install is invoked inside the supervisor's idle-harness gate and shared package-manager
// lock. It stages an exact approved version, verifies it, then atomically selects it. Previous
// versions and external installations are never removed or changed.
func (m *Manager) Install(ctx context.Context, spec Spec, update bool) (string, error) {
	s := m.RefreshSoftware(ctx, spec, true)
	if !update && s.InstalledVersion != "" {
		if s.Compatibility == "incompatible" {
			return "", fmt.Errorf("%s", s.Message)
		}
		return s.InstalledVersion, nil
	}
	target := s.RecommendedVersion
	if target == "" {
		if s.RequiresDaemonUpdate {
			return "", fmt.Errorf("update Mindwire service to %s before installing %s", s.RequiredDaemonVersion, spec.Name)
		}
		return "", fmt.Errorf("no tested %s version is available for this Mindwire service", spec.Name)
	}
	if s.InstalledVersion != "" && (!validVersion(s.InstalledVersion) || compare(s.InstalledVersion, target) > 0) {
		return "", fmt.Errorf("installed %s is newer than the tested version %s; it was left unchanged", spec.Name, target)
	}
	if s.InstalledVersion == target && s.Compatibility == "supported" {
		return target, nil
	}
	defer m.invalidate(spec)
	versions := filepath.Join(m.root, spec.Binary, "versions")
	if err := os.MkdirAll(versions, 0700); err != nil {
		return "", err
	}
	destination := filepath.Join(versions, target)
	path := versionBinary(m.root, spec.Binary, target)
	if output, err := versionOutput(ctx, spec, path); err != nil || ParseVersion(output) != target {
		stage, err := os.MkdirTemp(versions, ".install-*")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(stage)
		if err := m.install(ctx, spec, target, stage); err != nil {
			return "", err
		}
		stagedPath := filepath.Join(stage, "bin", spec.Binary)
		if filepath.Ext(path) == ".cmd" {
			stagedPath = filepath.Join(stage, spec.Binary+".cmd")
		}
		output, err := versionOutput(ctx, spec, stagedPath)
		if err != nil {
			return "", fmt.Errorf("installed %s could not start: %w", spec.Name, err)
		}
		if ParseVersion(output) != target {
			return "", fmt.Errorf("installed %s version does not match the approved version %s", spec.Name, target)
		}
		if _, err := os.Stat(destination); err == nil {
			// Retain a damaged old copy for diagnosis; never overwrite a directory in place.
			if err := os.Rename(destination, destination+fmt.Sprintf(".replaced-%d", time.Now().UnixNano())); err != nil {
				return "", err
			}
		}
		if err := os.Rename(stage, destination); err != nil {
			return "", err
		}
	}
	data, _ := json.Marshal(selection{Version: target, Environment: spec.Environment})
	if err := atomicWrite(filepath.Join(m.root, spec.Binary, "selected.json"), data); err != nil {
		return "", err
	}
	return target, nil
}

func installNPM(ctx context.Context, spec Spec, version, prefix string) error {
	cmd := exec.CommandContext(ctx, "bash", "-lc", "npm install --global --no-audit --no-fund --registry https://registry.npmjs.org --prefix "+quote(prefix)+" "+quote(spec.Package+"@"+version))
	proc.Group(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("install %s %s: %w\n%s", spec.Name, version, err, strings.TrimSpace(string(out)))
	}
	return nil
}
