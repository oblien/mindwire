package toolchain

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestAdmissionDetectsBinaryChangedOutsideDaemonDespiteCachedReadiness(t *testing.T) {
	m, spec := fixtureManager(t)
	m.install = fakeInstall
	version, err := m.Install(context.Background(), spec, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.CheckAdmission(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	path := versionBinary(m.root, spec.Binary, version)
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho 2.0.0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := m.CheckAdmission(context.Background(), spec); err == nil {
		t.Fatal("admission reused readiness from a different binary")
	}
}

func TestCancelledVersionProbeIsNotCachedAsAVersionMismatch(t *testing.T) {
	m, spec := fixtureManager(t)
	m.install = fakeInstall
	if _, err := m.Install(context.Background(), spec, false); err != nil {
		t.Fatal(err)
	}
	m.invalidate(spec)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	failed := m.Software(ctx, spec)
	if failed.Compatibility != "unavailable" || strings.Contains(failed.Message, "installation changed") {
		t.Fatalf("a cancelled probe was presented as corruption: %+v", failed)
	}
	if ready := m.Software(context.Background(), spec); ready.Compatibility != "supported" {
		t.Fatalf("a transient probe poisoned readiness: %+v", ready)
	}
}

func TestExplicitRepairCanReplaceDamagedManagedVersionWithoutTouchingExternalCLI(t *testing.T) {
	m, spec := fixtureManager(t)
	m.install = fakeInstall
	version, err := m.Install(context.Background(), spec, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(versionBinary(m.root, spec.Binary, version), []byte("#!/bin/sh\necho 9.0.0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	m.invalidate(spec)
	software := m.Software(context.Background(), spec)
	if !software.RepairAvailable || !software.UpdateAvailable {
		t.Fatalf("repair is inaccessible: %+v", software)
	}
	if _, err := m.Install(context.Background(), spec, true); err != nil {
		t.Fatal(err)
	}
	if err := m.CheckAdmission(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
}
