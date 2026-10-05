package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/registry"
)

func TestProjectLibraryHTTPAuthenticatedTwoClients(t *testing.T) {
	h, _, a := newRegistryAPIEnv(t)
	for _, method := range []string{"GET", "PATCH"} {
		if r := serve(t, h, method, "/workspace/project-library", "{}"); r.Code != 401 {
			t.Fatalf("unauthenticated %s: %d", method, r.Code)
		}
	}
	authorized := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer workspace-secret")
		h.ServeHTTP(w, r)
	})
	if err := a.registry.Import(registry.Import{Projects: []registry.Project{{Record: registry.Record{ID: "p"}, Name: "App", Path: "/work/app"}}}); err != nil {
		t.Fatal(err)
	}
	body := `{"expectedRevision":0,"folders":[{"id":"work","name":"Work"}],"placements":[{"projectId":"p","folderId":"work"}],"order":["p"]}`
	first := serve(t, authorized, "PATCH", "/workspace/project-library", body)
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body.String())
	}
	var saved registry.ProjectLibrary
	if err := json.Unmarshal(first.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	second := serve(t, authorized, "GET", "/workspace/project-library?agent=claude-code", "")
	if second.Code != 200 || second.Body.String() != first.Body.String() {
		t.Fatal("a second client/harness did not see the shared folder")
	}
	if repeat := serve(t, authorized, "PATCH", "/workspace/project-library", body); repeat.Body.String() != first.Body.String() {
		t.Fatal("retry duplicated an edit")
	}
	if conflict := serve(t, authorized, "PATCH", "/workspace/project-library", `{"expectedRevision":0,"folders":[{"id":"work","name":"Stale"}]}`); conflict.Code != 409 {
		t.Fatal("stale client overwrote folder", conflict.Code)
	}
	cleared := serve(t, authorized, "PATCH", "/workspace/project-library", fmt.Sprintf(`{"expectedRevision":%d,"placements":[{"projectId":"p","folderId":null}]}`, saved.Revision))
	if cleared.Code != 200 {
		t.Fatal(cleared.Body.String())
	}
	var library registry.ProjectLibrary
	if err := json.Unmarshal(cleared.Body.Bytes(), &library); err != nil {
		t.Fatal(err)
	}
	if len(library.Membership) != 0 || !reflect.DeepEqual(library.Folders, saved.Folders) || !reflect.DeepEqual(library.Order, saved.Order) {
		t.Fatal("returning to the flat list changed folder/order", library)
	}
}
