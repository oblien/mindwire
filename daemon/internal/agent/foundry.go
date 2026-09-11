package agent

import (
	"fmt"
	"net/url"
	"strings"
)

// FoundryBaseURL accepts a resource endpoint or an API base URL. Codex and Claude
// use different API paths; accepting the other one's URL produces a broken setup.
func FoundryBaseURL(base, resource, resourceDomain, apiPath string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		resource = strings.TrimSpace(resource)
		if resource == "" {
			return "", fmt.Errorf("enter your Foundry endpoint")
		}
		if len(resource) > 63 || strings.HasPrefix(resource, "-") || strings.HasSuffix(resource, "-") {
			return "", fmt.Errorf("enter a valid Azure resource name")
		}
		for _, c := range strings.ToLower(resource) {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return "", fmt.Errorf("enter a resource name, such as my-resource, without a URL or domain")
			}
		}
		base = "https://" + resource + "." + resourceDomain
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return "", fmt.Errorf("Microsoft Foundry endpoint must be a valid HTTPS URL")
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", fmt.Errorf("use the Foundry base URL ending in %s, without query parameters or a fragment", apiPath)
	}
	path := strings.TrimRight(u.Path, "/")
	if path == "" {
		u.Path = apiPath
	} else if !strings.HasSuffix(path, apiPath) || u.RawPath != "" {
		return "", fmt.Errorf("this agent needs a Foundry base URL ending in %s; copy the resource endpoint or the matching API base URL", apiPath)
	} else {
		u.Path = path
	}
	return u.String(), nil
}
