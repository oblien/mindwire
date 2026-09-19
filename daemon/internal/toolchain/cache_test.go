package toolchain

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCatalogSurvivesOutageRestartAndInvalidUpdates(t *testing.T) {
	catalog := policyFixture()
	var body atomic.Value
	data, _ := json.Marshal(catalog)
	body.Store(data)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body.Load().([]byte)) }))
	defer server.Close()
	root := t.TempDir()
	c := newCatalogCache(root, server.URL)
	if err := c.refresh(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	restarted := newCatalogCache(root, server.URL)
	if got, source, _ := restarted.snapshot(); got.Revision != catalog.Revision || source != "cached" {
		t.Fatalf("lost cache: %d %s", got.Revision, source)
	}
	for _, bad := range [][]byte{[]byte(`{"schemaVersion":999,"revision":999,"harnesses":{}}`), []byte(`not json`), bundled} {
		body.Store(bad)
		restarted.retryAfter = time.Time{}
		if err := restarted.refresh(context.Background(), true); err == nil {
			t.Fatal("accepted invalid or older policy")
		}
		if got, _, _ := restarted.snapshot(); got.Revision != catalog.Revision {
			t.Fatal("discarded last good policy")
		}
	}
	server.Close()
	restarted.retryAfter = time.Time{}
	if err := restarted.refresh(context.Background(), true); err == nil {
		t.Fatal("expected offline error")
	}
	if got, _, _ := restarted.snapshot(); got.Evaluate("codex", "0.1.16", "1.0.0").RecommendedVersion != "1.1.0" {
		t.Fatal("outage lost approved target")
	}
	// The cache contains metadata only, not credentials or executable installer scripts.
	if _, err := os.Stat(c.path); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogRequestsCoalesceAndDoNotRefetchOnNavigation(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); <-release; _, _ = w.Write(bundled) }))
	defer server.Close()
	c := newCatalogCache(t.TempDir(), server.URL)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() { _ = c.refresh(context.Background(), false) })
	}
	for i := 0; i < 100 && calls.Load() == 0; i++ {
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()
	for range 12 {
		if err := c.refresh(context.Background(), false); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("%d requests", calls.Load())
	}
}

func TestBundledFallbackWorksWithoutNetworkOrDiskCache(t *testing.T) {
	c := newCatalogCache(t.TempDir(), "off")
	if err := c.refresh(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	got, source, _ := c.snapshot()
	if source != "bundled" || got.Evaluate("codex", "0.1.16", "").RecommendedVersion != "0.155.0" {
		t.Fatalf("%s %+v", source, got)
	}
}
