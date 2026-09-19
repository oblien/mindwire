package toolchain

import (
	"context"
	"os"
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
