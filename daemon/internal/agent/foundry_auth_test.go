package agent

import (
	"maps"
	"testing"
)

func TestFoundryAuthExplicitChoicesFilterHiddenDrafts(t *testing.T) {
	spec := FoundryAuthSpec{Prefix: "cloud", AllowAzureCredentials: true}
	for _, endpoint := range []string{"url", "resource"} {
		for _, auth := range []string{"apiKey", "entraToken", "azureCredentials"} {
			t.Run(endpoint+"/"+auth, func(t *testing.T) {
				input := map[string]string{
					"cloudEndpointType": endpoint, "cloudAuthType": auth,
					"cloudBaseUrl": " https://example.test ", "cloudResource": " resource ",
					"cloudApiKey": " key ", "cloudAuthToken": " token ", "cloudModel": " deployment ",
					"unknown": "not-forwarded",
				}
				before := maps.Clone(input)
				got, err := spec.NormalizeInput(input)
				if err != nil {
					t.Fatal(err)
				}
				want := map[string]string{"cloudEndpointType": endpoint, "cloudAuthType": auth, "cloudModel": "deployment"}
				if endpoint == "url" {
					want["cloudBaseUrl"] = "https://example.test"
				} else {
					want["cloudResource"] = "resource"
				}
				switch auth {
				case "apiKey":
					want["cloudApiKey"] = "key"
				case "entraToken":
					want["cloudAuthToken"] = "token"
				}
				if !maps.Equal(got, want) || !maps.Equal(input, before) {
					t.Fatalf("selection did not isolate active fields: got %v, want %v", got, want)
				}
			})
		}
	}
}

func TestFoundryAuthRejectsInvalidOrMissingActiveInputs(t *testing.T) {
	spec := FoundryAuthSpec{Prefix: "cloud"}
	valid := map[string]string{
		"cloudEndpointType": "url", "cloudAuthType": "apiKey", "cloudBaseUrl": "https://example.test",
		"cloudResource": "hidden-resource", "cloudApiKey": "key", "cloudAuthToken": "hidden-token", "cloudModel": "deployment",
	}
	for _, changes := range []map[string]string{
		{"cloudEndpointType": "unknown"}, {"cloudEndpointType": ""},
		{"cloudAuthType": "unknown"}, {"cloudAuthType": ""}, {"cloudAuthType": "azureCredentials"},
		{"cloudBaseUrl": ""}, {"cloudEndpointType": "resource", "cloudResource": " \n "},
		{"cloudApiKey": ""}, {"cloudAuthType": "entraToken", "cloudAuthToken": ""}, {"cloudModel": " \n "},
	} {
		input := maps.Clone(valid)
		maps.Copy(input, changes)
		if _, err := spec.NormalizeInput(input); err == nil {
			t.Errorf("accepted invalid selection or used hidden fallback: %v", changes)
		}
	}
}

func TestNormalizeAuthInputUsesDefaultsAndActiveRequiredUnless(t *testing.T) {
	fields := []Field{
		{Key: "mode", Type: FieldSelect, Required: true, Default: "key", Options: []Option{{Value: "key"}, {Value: "token"}}},
		{Key: "key", Label: "API key", Type: FieldSecret, Required: true, RequiredUnless: []string{"token"}},
		{Key: "token", Type: FieldSecret, VisibleWhen: &FieldCondition{Key: "mode", Equals: "token"}},
	}
	if _, err := NormalizeAuthInput(fields, map[string]string{"token": "hidden"}); err == nil {
		t.Fatal("a hidden token satisfied requiredUnless")
	}
	got, err := NormalizeAuthInput(fields, map[string]string{"key": " key ", "token": "hidden"})
	if err != nil || !maps.Equal(got, map[string]string{"mode": "key", "key": "key"}) {
		t.Fatalf("defaults/visibility mismatch: %v, %v", got, err)
	}
}
