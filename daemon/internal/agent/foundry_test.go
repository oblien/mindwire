package agent

import "testing"

func TestFoundryBaseURL(t *testing.T) {
	for _, api := range []struct{ path, domain string }{
		{"/openai/v1", "openai.azure.com"},
		{"/anthropic", "services.ai.azure.com"},
	} {
		t.Run(api.path, func(t *testing.T) {
			for _, tc := range []struct{ name, base, resource, want string }{
				{"full URL", " https://example.services.ai.azure.com" + api.path + "/ ", "", "https://example.services.ai.azure.com" + api.path},
				{"resource endpoint", "https://example.services.ai.azure.com/", "", "https://example.services.ai.azure.com" + api.path},
				{"resource name", "", "my-resource", "https://my-resource." + api.domain + api.path},
				{"URL wins", "https://example.services.ai.azure.com" + api.path, "ignored", "https://example.services.ai.azure.com" + api.path},
				{"custom domain", "https://gateway.example/azure" + api.path, "", "https://gateway.example/azure" + api.path},
			} {
				t.Run(tc.name, func(t *testing.T) {
					got, err := FoundryBaseURL(tc.base, tc.resource, api.domain, api.path)
					if err != nil || got != tc.want {
						t.Fatalf("got %q, %v; want %q", got, err, tc.want)
					}
				})
			}
			for _, base := range []string{"http://example.test", "https://", "https://user:secret@example.test", "https://example.test/wrong-api", "https://example.test" + api.path + "/responses", "https://example.test" + api.path + "?api-version=old", "https://example.test" + api.path + "#fragment", "https://example.test" + api.path + "?"} {
				if _, err := FoundryBaseURL(base, "", api.domain, api.path); err == nil {
					t.Errorf("accepted invalid base URL %q", base)
				}
			}
			for _, resource := range []string{"", "my.resource", "-resource", "resource-", "some/resource", "resource\nname", "https://example.test"} {
				if _, err := FoundryBaseURL("", resource, api.domain, api.path); err == nil {
					t.Errorf("accepted invalid resource %q", resource)
				}
			}
		})
	}
}
