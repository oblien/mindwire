package mindwire

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectIconUsesTheSharedWorkspaceMetadata(t *testing.T) {
	dir := t.TempDir()
	c, err := New(Options{StatePath: filepath.Join(t.TempDir(), "state.json"), CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M0 0h24v24H0z"/></svg>`
	name := "logo.svg"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(svg), 0600); err != nil {
		t.Fatal(err)
	}
	initial, err := c.Workspace.Projects.Put("project", WorkspaceProject{Name: "App", Path: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := c.Workspace.SetProjectIcon("project", &name, initial.Projects[0].Revision)
	if err != nil || saved.Projects[0].IconPath == nil {
		t.Fatalf("save: %+v %v", saved, err)
	}
	icon, err := c.Workspace.ProjectIcon("project", "")
	if err != nil || string(icon.Content) != svg || icon.MediaType != "image/svg+xml" {
		t.Fatalf("icon: %+v %v", icon, err)
	}
	cleared, err := c.Workspace.SetProjectIcon("project", nil, saved.Projects[0].Revision)
	if err != nil || cleared.Projects[0].IconPath != nil {
		t.Fatalf("clear: %+v %v", cleared, err)
	}
	if c.Health().ProjectIconsVersion != 1 {
		t.Fatal("project icons capability is missing")
	}
}
