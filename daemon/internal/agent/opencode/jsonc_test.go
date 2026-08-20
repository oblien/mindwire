package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestStripJSONC covers the JSONC features opencode accepts (comments + trailing commas) and the
// cases that must be left ALONE (markers/commas inside string literals, escaped quotes).
func TestStripJSONC(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"trailing comma object", `{"a":1,}`},
		{"trailing comma array", `{"a":[1,2,]}`},
		{"trailing comma nested", `{"a":{"b":1,},"c":[1,],}`},
		{"line comment", "{\n  // pick a model\n  \"model\": \"x\"\n}"},
		{"block comment", `{/* header */ "a": 1 }`},
		{"comment before trailing comma", `{"a":1, /* trailing */ }`},
		{"slashes inside string kept", `{"url":"https://example.com/v1","note":"a,}"}`},
		{"comment marker inside string", `{"s":"not // a comment","t":"/* nope */"}`},
		{"escaped quote inside string", `{"s":"he said \"hi\",","n":1,}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clean := stripJSONC([]byte(tc.in))
			var v map[string]any
			if err := json.Unmarshal(clean, &v); err != nil {
				t.Fatalf("sanitized JSON did not parse: %v\ngot: %s", err, clean)
			}
		})
	}
}

// TestStripJSONCPreservesStringContent asserts the sanitizer only removes comments/commas and never
// mangles a string value that happens to contain those tokens.
func TestStripJSONCPreservesStringContent(t *testing.T) {
	in := `{"url":"https://x.dev/a//b","csv":"a,b,","glob":"/* */"}`
	var v struct {
		URL  string `json:"url"`
		CSV  string `json:"csv"`
		Glob string `json:"glob"`
	}
	if err := json.Unmarshal(stripJSONC([]byte(in)), &v); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v.URL != "https://x.dev/a//b" || v.CSV != "a,b," || v.Glob != "/* */" {
		t.Fatalf("string content altered: %+v", v)
	}
}

// TestReadTopLevelTolerantJSONC is the end-to-end guard for the reported bug: a JSONC opencode.json
// (trailing comma + comment) must round-trip through the provider/mcp readers instead of failing with
// "invalid character '}' looking for beginning of object key string".
func TestReadTopLevelTolerantJSONC(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	jsonc := "{\n" +
		"  // user's hand-edited config\n" +
		"  \"$schema\": \"https://opencode.ai/config.json\",\n" +
		"  \"theme\": \"dark\", /* keep it */\n" +
		"  \"provider\": {\n" +
		"    \"acme\": { \"npm\": \"@ai-sdk/openai-compatible\", \"options\": { \"baseURL\": \"https://acme.dev/v1\" }, },\n" +
		"  },\n" +
		"}\n"
	if err := os.WriteFile(path, []byte(jsonc), 0o600); err != nil {
		t.Fatal(err)
	}

	top, err := readTopLevel(path)
	if err != nil {
		t.Fatalf("readTopLevel on JSONC failed: %v", err)
	}
	if _, ok := top["$schema"]; !ok {
		t.Errorf("sibling key $schema lost")
	}
	if _, ok := top["theme"]; !ok {
		t.Errorf("sibling key theme lost")
	}

	blocks, err := readSubtree(path, "provider")
	if err != nil {
		t.Fatalf("readSubtree(provider): %v", err)
	}
	if _, ok := blocks["acme"]; !ok {
		t.Fatalf("provider.acme not read from JSONC; got %v", keysOf(blocks))
	}
}

// TestReadTopLevelBrokenSurfacesOriginalError ensures a genuinely-corrupt file still errors (and with
// the strict, pre-sanitize message that points at the real offending byte).
func TestReadTopLevelBrokenSurfacesOriginalError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	if err := os.WriteFile(path, []byte(`{"a": }`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readTopLevel(path); err == nil {
		t.Fatal("expected an error for a structurally-broken config")
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
