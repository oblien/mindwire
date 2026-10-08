package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/toolchain"
)

// Command describes an action, a settings selector, or a native prompt/skill. Clients
// must route by Kind: a settings command is never sent to the model as ordinary text.
type Command struct {
	Name             string   `json:"name"`
	Label            string   `json:"label"`
	Description      string   `json:"description,omitempty"`
	Kind             string   `json:"kind"` // settings | compact | prompt | new_chat | history | rename
	Aliases          []string `json:"aliases,omitempty"`
	ArgumentHint     string   `json:"argumentHint,omitempty"`
	AcceptsArguments bool     `json:"acceptsArguments,omitempty"`
	SettingCanons    []string `json:"settingCanons,omitempty"`
	RequiresIdle     bool     `json:"requiresIdle,omitempty"`
	RequiresSession  bool     `json:"requiresSession,omitempty"`
	// Native dispatch details stay on the server. Clients send only the advertised name.
	Input     string `json:"-"`
	SkillPath string `json:"-"`
}

type CommandCatalog struct {
	Commands []Command `json:"commands"`
	Warning  string    `json:"warning,omitempty"`
}

type CommandQuery struct {
	CWD     string
	Config  map[string]string
	Env     map[string]string
	Refresh bool `json:"-"`
}

type CommandInvocation struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
}

func (c CommandInvocation) Text() string {
	text := "/" + c.Name
	if args := strings.TrimSpace(c.Arguments); args != "" {
		text += " " + args
	}
	return text
}

// CommandsModule discovers the installed harness's command surface without a model turn.
type CommandsModule interface {
	Commands(context.Context, CommandQuery) (CommandCatalog, error)
}

func ValidCommandName(name string) bool {
	if name == "" || len(name) > 200 || strings.HasPrefix(name, "_") {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.:", r)) {
			return false
		}
	}
	return true
}

func (c Command) Matches(name string) bool {
	if c.Name == name {
		return true
	}
	for _, alias := range c.Aliases {
		if alias == name {
			return true
		}
	}
	return false
}

// PrepareCommand resolves against fresh-enough native metadata, including exact skill
// paths. A stale/unsupported selection fails explicitly instead of becoming a model prompt.
func PrepareCommand(ctx context.Context, adapter Adapter, in TurnInput) (TurnInput, error) {
	invocation := in.Options.Command
	if invocation == nil {
		return in, nil
	}
	mod, ok := adapter.(CommandsModule)
	if !ok || !ValidCommandName(invocation.Name) {
		return in, fmt.Errorf("this harness does not support that command")
	}
	catalog, err := mod.Commands(ctx, CommandQuery{CWD: in.CWD, Config: in.Config, Env: in.Env})
	if err != nil {
		return in, err
	}
	for _, command := range catalog.Commands {
		if !command.Matches(invocation.Name) {
			continue
		}
		if command.Kind != "prompt" || command.Input == "" {
			return in, fmt.Errorf("/%s opens a control; use its dedicated action", command.Name)
		}
		arguments := strings.TrimSpace(invocation.Arguments)
		if arguments != "" && !command.AcceptsArguments {
			return in, fmt.Errorf("/%s does not accept arguments", command.Name)
		}
		in.Message = command.Input
		if command.Input == "/"+command.Name {
			in.Message = "/" + invocation.Name // Let the native CLI keep its advertised alias in history.
		}
		if arguments != "" {
			in.Message += " " + arguments
		}
		in.Command = &command
		return in, nil
	}
	return in, fmt.Errorf("/%s is no longer available; refresh the command list", invocation.Name)
}

// FilterSettingsCommands omits controls whose fields the installed CLI doesn't expose.
func FilterSettingsCommands(commands []Command, schema SettingsSchema) []Command {
	known := map[string]bool{}
	for _, section := range schema.Sections {
		for _, field := range section.Fields {
			if field.Type != FieldSecret {
				known[field.Canon] = true
			}
		}
	}
	out := make([]Command, 0, len(commands))
	for _, command := range commands {
		if command.Kind == "settings" && len(command.SettingCanons) > 0 {
			fields := []string{}
			for _, canon := range command.SettingCanons {
				if known[canon] {
					fields = append(fields, canon)
				}
			}
			if len(fields) == 0 {
				continue
			}
			command.SettingCanons = fields
		}
		out = append(out, command)
	}
	return out
}

// CommandCache performs bounded, on-demand, single-flight discovery. No idle worker,
// transcript reads or model requests. Account/config/cwd/executable changes get a new key.
type CommandCache struct {
	mu      sync.Mutex
	entries map[string]*commandCacheEntry
}

type commandCacheEntry struct {
	done    chan struct{}
	until   time.Time
	catalog CommandCatalog
	err     error
}

func CommandCacheKey(binary string, query CommandQuery) string {
	path := toolchain.Executable(binary)
	if resolved, err := exec.LookPath(path); err == nil {
		path = resolved
	}
	var modified int64
	if stat, err := os.Stat(path); err == nil {
		modified = stat.ModTime().UnixNano()
	}
	data, _ := json.Marshal([]any{path, modified, query})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (c *CommandCache) Get(ctx context.Context, key string, refresh bool, fetch func(context.Context) (CommandCatalog, error)) (CommandCatalog, error) {
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]*commandCacheEntry{}
	}
	entry := c.entries[key]
	if entry != nil && !entry.until.IsZero() && (refresh || time.Now().After(entry.until)) {
		delete(c.entries, key)
		entry = nil
	}
	if entry == nil {
		if len(c.entries) >= 24 {
			oldestKey := ""
			for k, value := range c.entries {
				if !value.until.IsZero() && (oldestKey == "" || value.until.Before(c.entries[oldestKey].until)) {
					oldestKey = k
				}
			}
			if oldestKey == "" {
				c.mu.Unlock()
				return CommandCatalog{}, fmt.Errorf("command discovery is busy; try again shortly")
			}
			delete(c.entries, oldestKey)
		}
		entry = &commandCacheEntry{done: make(chan struct{})}
		c.entries[key] = entry
		go func() {
			work, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			catalog, err := fetch(work)
			ttl := 2 * time.Minute
			if err != nil || catalog.Warning != "" {
				ttl = 10 * time.Second
			}
			c.mu.Lock()
			entry.catalog, entry.err, entry.until = catalog, err, time.Now().Add(ttl)
			close(entry.done)
			c.mu.Unlock()
		}()
	}
	c.mu.Unlock()
	select {
	case <-entry.done:
		return entry.catalog, entry.err
	case <-ctx.Done():
		return CommandCatalog{}, ctx.Err()
	}
}
