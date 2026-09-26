package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/projecticon"
	"github.com/oblien/mindwire/daemon/internal/registry"
)

func TestProjectIconsAreSharedRevisionedMetadataAndOlderEditsPreserveThem(t *testing.T) {
	h, a, dir := gitHTTPFixture(t)
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#a9dc76" d="M0 0h24v24H0z"/></svg>`
	if err := os.WriteFile(filepath.Join(dir, "icon.svg"), []byte(svg), 0600); err != nil {
		t.Fatal(err)
	}
	preview := serve(t, h, "GET", "/workspace/projects/project/icon?path=icon.svg", "")
	var image projecticon.Image
	if preview.Code != http.StatusOK || json.Unmarshal(preview.Body.Bytes(), &image) != nil || string(image.Content) != svg || image.MediaType != "image/svg+xml" {
		t.Fatalf("icon preview: %d %s", preview.Code, preview.Body)
	}
	p, _ := a.registry.Project("project")
	put := func(name string, revision int64, icon string) registry.Snapshot {
		t.Helper()
		body := fmt.Sprintf(`{"record":{"name":%q,"path":%q%s},"expectedRevision":%d}`, name, dir, icon, revision)
		response := serve(t, h, "PUT", "/workspace/projects/project", body)
		var snapshot registry.Snapshot
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &snapshot) != nil {
			t.Fatalf("save icon: %d %s", response.Code, response.Body)
		}
		return snapshot
	}
	saved := put(p.Name, p.Revision, `,"iconPath":"icon.svg"`)
	if saved.Projects[0].IconPath == nil || *saved.Projects[0].IconPath != "icon.svg" {
		t.Fatal("icon not synchronized")
	}
	// Old clients know neither iconPath nor its endpoint. Renaming must retain it.
	renamed := put("Renamed project", saved.Projects[0].Revision, "")
	if renamed.Projects[0].IconPath == nil {
		t.Fatal("older metadata edit removed icon")
	}
	current := serve(t, h, "GET", "/workspace/projects/project/icon", "")
	if current.Code != 200 || json.Unmarshal(current.Body.Bytes(), &image) != nil || string(image.Content) != svg {
		t.Fatal("saved icon cannot be read")
	}
	stale := serve(t, h, "PUT", "/workspace/projects/project", fmt.Sprintf(`{"record":{"name":"Stale","path":%q,"iconPath":null},"expectedRevision":%d}`, dir, saved.Projects[0].Revision))
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale icon edit accepted: %d %s", stale.Code, stale.Body)
	}
	cleared := put("Renamed project", renamed.Projects[0].Revision, `,"iconPath":null`)
	if cleared.Projects[0].IconPath != nil {
		t.Fatal("explicit reset did not clear icon")
	}
	if response := serve(t, h, "GET", "/workspace/projects/project/icon", ""); response.Code != 404 {
		t.Fatalf("cleared icon: %d", response.Code)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "icon.svg")); err != nil || string(data) != svg {
		t.Fatal("metadata reset modified the project file")
	}
}

func TestProjectIconRejectsEscapesMissingFilesAndOversizedOrExternalSVG(t *testing.T) {
	h, _, dir := gitHTTPFixture(t)
	outside := filepath.Join(t.TempDir(), "secret.svg")
	if err := os.WriteFile(outside, []byte(`<svg/>`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "escape.svg")); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"large.svg":    strings.Repeat("x", projecticon.MaxBytes+1),
		"invalid.png":  "not an image",
		"external.svg": `<svg><use href="https://example.invalid/private.svg#icon"/></svg>`,
		"script.svg":   `<svg><script>alert(1)</script></svg>`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"../secret.svg", outside, ".git/icon.svg", "escape.svg", "missing.png", "large.svg", "invalid.png", "external.svg", "script.svg"} {
		response := serve(t, h, "GET", "/workspace/projects/project/icon?path="+url.QueryEscape(name), "")
		if response.Code != http.StatusBadRequest {
			t.Fatalf("accepted %q: %d %s", name, response.Code, response.Body)
		}
	}
}
