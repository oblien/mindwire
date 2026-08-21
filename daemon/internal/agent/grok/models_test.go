package grok

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestModelsUsesAuthenticatedXAIModelsAPI(t *testing.T) {
	oldURL, oldClient := xaiModelsURL, xaiHTTPClient
	xaiModelsURL = "https://models.example.test/v1/models"
	xaiHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got, want := r.Header.Get("Authorization"), "Bearer xai-test"; got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %q", r.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"grok-build"},{"id":"grok-4.5"}]}`)), Header: make(http.Header)}, nil
	})}
	t.Cleanup(func() { xaiModelsURL, xaiHTTPClient = oldURL, oldClient })

	models, err := (adapter{}).Models(map[string]string{"XAI_API_KEY": "xai-test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "grok-build" || models[0].Provider != "xai" {
		t.Fatalf("models = %#v", models)
	}
}

func TestModelsWithoutCredentialIsEmpty(t *testing.T) {
	models, err := (adapter{}).Models(nil)
	if err != nil || len(models) != 0 {
		t.Fatalf("models = %#v, err = %v", models, err)
	}
}

func TestModelsIncludesConfiguredCustomAliases(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GROK_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[model.private]\nmodel = \"private-real-id\"\nname = \"Private router\"\n\n[model.\"with.dot\"]\nmodel = \"other\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	models, err := (adapter{}).Models(nil)
	if err != nil || len(models) != 2 {
		t.Fatalf("models = %#v, err = %v", models, err)
	}
	if models[0].ID != "private" || models[0].Label != "Private router" || models[1].ID != "with.dot" {
		t.Fatalf("configured models = %#v", models)
	}
}
