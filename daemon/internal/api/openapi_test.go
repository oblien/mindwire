package api

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestOpenAPIRouteParity asserts daemon/openapi.json documents exactly the routes the daemon
// actually registers (Routes + PublicRoutes) — no more, no less. It guards the spec against
// drift when a route is added or removed. It checks route existence (method × path), not schema
// fidelity.
func TestOpenAPIRouteParity(t *testing.T) {
	data, err := os.ReadFile("../../openapi.json")
	if err != nil {
		t.Fatalf("read openapi.json: %v", err)
	}
	var spec struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("parse openapi.json: %v", err)
	}

	httpMethods := map[string]bool{"GET": true, "POST": true, "PUT": true, "DELETE": true, "PATCH": true}

	spec_ := map[string]bool{}
	for path, ops := range spec.Paths {
		for m := range ops {
			mu := strings.ToUpper(m)
			if httpMethods[mu] {
				spec_[mu+" "+path] = true
			}
		}
	}

	live := map[string]bool{}
	a := New(nil, nil, nil)
	for _, rt := range append(a.Routes(), PublicRoutes...) {
		live[rt.Method+" "+rt.Pattern] = true
	}

	for k := range live {
		if !spec_[k] {
			t.Errorf("route %q is registered but missing from openapi.json", k)
		}
	}
	for k := range spec_ {
		if !live[k] {
			t.Errorf("openapi.json documents %q but no such route is registered", k)
		}
	}
}
