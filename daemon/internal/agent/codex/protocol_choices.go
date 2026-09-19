package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/toolchain"
)

var protocolEnums struct {
	sync.Mutex
	loaded time.Time
	values map[string][]string
	path   string
}

// These options are absent from --help. Discover them from the installed server,
// so older Codex versions do not advertise settings they cannot accept.
func protocolChoices(name string) []string {
	protocolEnums.Lock()
	defer protocolEnums.Unlock()
	path := toolchain.Executable("codex")
	if protocolEnums.path == path && time.Since(protocolEnums.loaded) < 5*time.Minute {
		return protocolEnums.values[name]
	}
	protocolEnums.loaded, protocolEnums.values = time.Now(), map[string][]string{}
	protocolEnums.path = path
	dir, err := os.MkdirTemp("", "mindwire-protocol-")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if toolchain.CommandContext(ctx, "codex", "app-server", "generate-json-schema", "--experimental", "--out", dir).Run() != nil {
		return nil
	}
	for _, file := range []string{"ThreadStartParams.json", "TurnStartParams.json"} {
		data, err := os.ReadFile(filepath.Join(dir, "v2", file))
		if err != nil {
			continue
		}
		var schema struct {
			Definitions map[string]json.RawMessage `json:"definitions"`
		}
		if json.Unmarshal(data, &schema) != nil {
			continue
		}
		for key, definition := range schema.Definitions {
			if values := stringEnum(definition); len(values) > 0 {
				protocolEnums.values[key] = values
			}
		}
	}
	return protocolEnums.values[name]
}

// Some native settings combine a string enum with a structured alternative. Only
// the string choices belong in the settings picker; object property enums do not.
func stringEnum(raw json.RawMessage) []string {
	var node struct {
		Enum  []string          `json:"enum"`
		OneOf []json.RawMessage `json:"oneOf"`
		AnyOf []json.RawMessage `json:"anyOf"`
	}
	if json.Unmarshal(raw, &node) != nil {
		return nil
	}
	values := node.Enum
	for _, child := range append(node.OneOf, node.AnyOf...) {
		values = append(values, stringEnum(child)...)
	}
	return values
}
