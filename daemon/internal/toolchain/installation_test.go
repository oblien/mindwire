package toolchain

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallationDiscoveryFindsNativePATHWithoutExecutingTheCLI(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MINDWIRE_TOOLCHAIN_DIR", filepath.Join(dir, "managed"))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	spec := Spec{ID: "inventory-fixture", Binary: "mindwire-installation-fixture"}
	marker := filepath.Join(dir, "executed")
	binary := filepath.Join(dir, spec.Binary)
	if err := os.WriteFile(binary, []byte("#!/bin/sh\ntouch "+quote(marker)+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	state, err := InspectInstallation(context.Background(), spec)
	if err != nil || state != "installed" {
		t.Fatal(state, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("inventory executed a native harness")
	}
	if err := os.Remove(binary); err != nil {
		t.Fatal(err)
	}
	state, err = InspectInstallation(context.Background(), spec)
	if err != nil || state != "missing" {
		t.Fatal(state, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if state, err := InspectInstallation(ctx, spec); err == nil || state != "" {
		t.Fatal("cancelled discovery reported an uninstall", state, err)
	}
}

func TestManagedInstallationDiscoveryKeepsBrokenSelectionVisible(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MINDWIRE_TOOLCHAIN_DIR", dir)
	spec := Spec{ID: "managed-fixture", Binary: "mindwire-managed-fixture"}
	if err := os.MkdirAll(filepath.Join(dir, spec.Binary), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, spec.Binary, "selected.json"), []byte(`{"version":"1.2.3"}`), 0600); err != nil {
		t.Fatal(err)
	}
	state, err := InspectInstallation(context.Background(), spec)
	if err != nil || state != "repair" {
		t.Fatal("missing managed executable was hidden", state, err)
	}
	binary := versionBinary(dir, spec.Binary, "1.2.3")
	if err := os.MkdirAll(filepath.Dir(binary), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 99\n"), 0700); err != nil {
		t.Fatal(err)
	}
	state, err = InspectInstallation(context.Background(), spec)
	if err != nil || state != "installed" {
		t.Fatal(state, err)
	}
}
