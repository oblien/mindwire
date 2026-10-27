package claude

import (
	"encoding/json"
	"fmt"
)

// Keep native Claude definitions lossless while translating canonical product
// tools at the adapter boundary. Credential values never enter the overlay file.
func nativeMCPOverlay(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return raw, nil
	}
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(raw, &servers); err != nil {
		return nil, err
	}
	if wrapped, ok := servers["mcpServers"]; ok {
		servers = nil
		if err := json.Unmarshal(wrapped, &servers); err != nil {
			return nil, err
		}
	}
	for name, raw := range servers {
		var server map[string]any
		if err := json.Unmarshal(raw, &server); err != nil {
			return nil, fmt.Errorf("MCP server %s: %w", name, err)
		}
		if _, ok := server["url"]; ok {
			if _, ok := server["type"]; !ok {
				server["type"] = "http"
			}
			headers, _ := server["headers"].(map[string]any)
			if headers == nil {
				headers = map[string]any{}
			}
			if canonical, ok := server["httpHeaders"].(map[string]any); ok {
				for k, v := range canonical {
					headers[k] = v
				}
			}
			if variable, ok := server["bearerTokenEnvVar"].(string); ok && variable != "" {
				if _, exists := headers["Authorization"]; !exists {
					headers["Authorization"] = "Bearer ${" + variable + "}"
				}
			}
			if len(headers) > 0 {
				server["headers"] = headers
			}
		}
		delete(server, "bearerTokenEnvVar")
		delete(server, "httpHeaders")
		delete(server, "defaultToolsApprovalMode")
		servers[name], _ = json.Marshal(server)
	}
	return json.Marshal(map[string]any{"mcpServers": servers})
}
