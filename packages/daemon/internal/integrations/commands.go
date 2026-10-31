package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/oblien/mindwire/packages/daemon/internal/registry"
)

var commandName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
var argumentName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

func validateCommands(spec Spec) error {
	if len(spec.Commands) == 0 {
		return nil
	}
	if len(spec.Commands) > 25 || !commandName.MatchString(strings.TrimPrefix(spec.Command, "/")) {
		return invalid("command menus require a lowercase root command and at most 25 entries")
	}
	seen := map[string]bool{}
	for _, command := range spec.Commands {
		if !commandName.MatchString(command.Name) || command.Name == strings.TrimPrefix(spec.Command, "/") || seen[command.Name] || strings.TrimSpace(command.Description) == "" || utf8.RuneCountInString(command.Description) > 100 || strings.ContainsAny(command.Description, "\r\n\x00") || command.Argument != "" && !argumentName.MatchString(command.Argument) {
			return invalid("invalid or duplicate command definition")
		}
		if spec.Provider == "slack" && len(spec.Command)+1+len(command.Name) > 32 {
			return invalid("Slack command names must fit in 32 characters")
		}
		seen[command.Name] = true
	}
	return nil
}

// commandText translates only advertised aliases. Unknown slash commands cannot
// turn into assistant prompts. The existing sender allowlists and host grants still apply.
func commandText(c savedConnection, name, text string) (string, bool) {
	root := name
	if before, target, addressed := strings.Cut(name, "@"); addressed {
		if c.Provider != "telegram" || c.BotUsername == "" && len(c.Commands) > 0 || c.BotUsername != "" && !strings.EqualFold(target, c.BotUsername) {
			return "", false
		}
		root = before
	}
	if root == c.Command {
		return strings.TrimSpace(text), true
	}
	for _, command := range c.Commands {
		alias := "/" + command.Name
		if c.Provider == "slack" {
			alias = c.Command + "-" + command.Name
		}
		if root == alias {
			return strings.TrimSpace(command.Name + " " + text), true
		}
	}
	return "", false
}

type discordOption struct {
	Name    string          `json:"name"`
	Type    int             `json:"type"`
	Value   json.RawMessage `json:"value"`
	Options []discordOption `json:"options"`
}

func discordCommandText(c savedConnection, options []discordOption) (string, error) {
	// Keep legacy /root text:"verb argument" registrations working during upgrades.
	if len(options) == 0 {
		return "", nil
	}
	if len(options) != 1 {
		return "", invalid("invalid command options")
	}
	option := options[0]
	if option.Name == "message" || option.Name == "text" {
		var value string
		if (option.Type != 0 && option.Type != 3) || len(option.Options) > 0 || json.Unmarshal(option.Value, &value) != nil {
			return "", invalid("invalid command text")
		}
		return value, nil
	}
	if option.Type != 1 {
		return "", invalid("unsupported command option")
	}
	for _, command := range c.Commands {
		if command.Name != option.Name {
			continue
		}
		if command.Argument == "" {
			if len(option.Options) != 0 {
				return "", invalid("unexpected command argument")
			}
			return command.Name, nil
		}
		if len(option.Options) != 1 || option.Options[0].Name != command.Argument || option.Options[0].Type != 3 || len(option.Options[0].Options) > 0 {
			return "", invalid("invalid command argument")
		}
		var value string
		if json.Unmarshal(option.Options[0].Value, &value) != nil || strings.TrimSpace(value) == "" {
			return "", invalid("missing command argument")
		}
		return command.Name + " " + value, nil
	}
	return "", invalid("unknown command")
}

// SyncCommands uses only provider-owned endpoints and the saved bot credentials.
// It is explicit (never called by polling), so reconnects do not spam provider APIs.
// Discord updates one named guild command; Telegram updates this dedicated bot's menu.
func (s *Service) SyncCommands(ctx context.Context, id string, revision int64) error {
	s.commandMu.Lock()
	defer s.commandMu.Unlock()
	s.mu.Lock()
	c, err := s.load(id)
	closed := s.closed
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if closed {
		return errors.New("integrations are closed")
	}
	if c.Revision != revision {
		return registry.ErrConflict
	}
	if len(c.Commands) == 0 {
		return invalid("no command catalog configured")
	}
	if c.Provider == "slack" || c.Provider == "teams" {
		return invalid("this provider requires commands in its app manifest")
	}
	username := ""
	switch c.Provider {
	case "telegram":
		base := "https://api.telegram.org/bot" + c.Secrets.BotToken + "/"
		info, err := s.request(ctx, "POST", base+"getMe", "", map[string]any{})
		if err != nil {
			return err
		}
		var bot struct {
			Username string `json:"username"`
		}
		if json.Unmarshal(info["result"], &bot) != nil || bot.Username == "" {
			return errors.New("provider returned an invalid bot identity")
		}
		username = bot.Username
		menu := []map[string]string{{"command": strings.TrimPrefix(c.Command, "/"), "description": "Show commands and help"}}
		for _, command := range c.Commands {
			description := command.Description
			if command.Argument != "" {
				description = "<" + command.Argument + "> — " + description
			}
			menu = append(menu, map[string]string{"command": command.Name, "description": description})
		}
		if _, err = s.request(ctx, "POST", base+"setMyCommands", "", map[string]any{"commands": menu}); err != nil {
			return err
		}
	case "discord":
		options := []map[string]any{}
		for _, command := range c.Commands {
			option := map[string]any{"type": 1, "name": command.Name, "description": command.Description}
			if command.Argument != "" {
				option["options"] = []map[string]any{{"type": 3, "name": command.Argument, "description": "Enter the " + strings.ReplaceAll(command.Argument, "_", " "), "required": true}}
			}
			options = append(options, option)
		}
		endpoint := "https://discord.com/api/v10/applications/" + url.PathEscape(c.ApplicationID) + "/guilds/" + url.PathEscape(c.WorkspaceID) + "/commands"
		if _, err = s.request(ctx, "POST", endpoint, "Bot "+c.Secrets.BotToken, map[string]any{"type": 1, "name": strings.TrimPrefix(c.Command, "/"), "description": "Work with your apps", "options": options}); err != nil {
			return err
		}
	default:
		return invalid("unsupported command menu")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.load(id)
	if err != nil {
		return err
	}
	if current.Revision != revision {
		return registry.ErrConflict
	}
	if username != "" && current.BotUsername != username {
		current.BotUsername = username
		current.Revision++
		if err := s.db.SurfacePut(configKind, id, current); err != nil {
			return err
		}
		s.signal()
	}
	return nil
}
