package surface

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type helperAuthorization struct{ token string }

func (a helperAuthorization) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	copy.Header.Set("Authorization", "Bearer "+a.token)
	return http.DefaultTransport.RoundTrip(copy)
}

// DesktopToolHelper bridges command-only harnesses to the same run-scoped MCP
// tools. Provider tokens and the daemon owner credential never enter this path.
func DesktopToolHelper(ctx context.Context, args []string, out io.Writer) error {
	endpoint, err := url.Parse(os.Getenv("MINDWIRE_DESKTOP_URL"))
	token := os.Getenv(runTokenEnv)
	if err != nil || endpoint.Scheme != "http" || endpoint.Hostname() != "127.0.0.1" || endpoint.Path != "/mcp" || endpoint.User != nil || endpoint.RawQuery != "" || token == "" {
		return errors.New("desktop tools are available only inside an active Mindwire project run")
	}
	if len(args) < 1 || len(args) > 2 {
		return errors.New("usage: --desktop-tool list | TOOL_NAME 'JSON_ARGUMENTS'")
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "mindwire-desktop-helper", Version: "2"}, nil)
	connection, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint.String(),
		HTTPClient:           &http.Client{Transport: helperAuthorization{token}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		return errors.New("could not reach this run's desktop tools; the run may have ended")
	}
	defer connection.Close()
	if args[0] == "list" {
		result, err := connection.ListTools(ctx, nil)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(result)
	}
	if !strings.HasPrefix(args[0], "surface_") {
		return errors.New("choose a desktop tool from --desktop-tool list")
	}
	arguments := map[string]any{}
	if len(args) == 2 {
		if len(args[1]) > 2<<20 {
			return errors.New("desktop arguments are too large")
		}
		if err := json.Unmarshal([]byte(args[1]), &arguments); err != nil {
			return errors.New("desktop arguments must be a JSON object")
		}
	}
	result, err := connection.CallTool(ctx, &mcp.CallToolParams{Name: args[0], Arguments: arguments})
	if err != nil {
		return err
	}
	response := map[string]any{}
	for _, block := range result.Content {
		switch block := block.(type) {
		case *mcp.TextContent:
			var payload map[string]any
			if json.Unmarshal([]byte(block.Text), &payload) == nil {
				for key, value := range payload {
					response[key] = value
				}
			}
		case *mcp.ImageContent:
			directory := os.Getenv("MINDWIRE_DESKTOP_IMAGES")
			info, err := os.Lstat(directory)
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 || block.MIMEType != "image/png" || len(block.Data) > 16<<20 {
				return errors.New("this run's private screenshot directory is unavailable")
			}
			path := filepath.Join(directory, newID()+".png")
			if err := os.WriteFile(path, block.Data, 0600); err != nil {
				return err
			}
			response["imagePath"] = path
		}
	}
	response["isError"] = result.IsError
	if err := json.NewEncoder(out).Encode(response); err != nil {
		return err
	}
	if result.IsError {
		return fmt.Errorf("desktop tool %s did not complete; see its structured error", args[0])
	}
	return nil
}
