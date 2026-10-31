package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/oblien/mindwire/packages/daemon/internal/registry"
)

func menuSpec(f *fixture, provider string) Spec {
	spec := f.spec(provider)
	spec.Dispatch = "host"
	spec.BotID = ""
	spec.Command = "/example"
	spec.Commands = []CommandDefinition{{Name: "projects", Description: "List your projects"}, {Name: "status", Description: "Check deployment status", Argument: "project_id"}, {Name: "ask", Description: "Talk to your assistant", Argument: "message"}}
	return spec
}

func TestNativeCommandMenus(t *testing.T) {
	for _, provider := range []string{"telegram", "discord"} {
		t.Run(provider, func(t *testing.T) {
			f := newFixture(t)
			c, err := f.s.Save("", menuSpec(f, provider))
			if err != nil {
				t.Fatal(err)
			}
			calls := []string{}
			f.s.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				calls = append(calls, r.URL.Host+r.URL.Path)
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if provider == "telegram" {
					if r.URL.Path == "/bottest-bot-token/getMe" {
						return response(200, `{"ok":true,"result":{"username":"ExampleBot"}}`), nil
					}
					if r.URL.Host != "api.telegram.org" || r.URL.Path != "/bottest-bot-token/setMyCommands" {
						t.Fatal("unexpected menu endpoint")
					}
					commands := body["commands"].([]any)
					if len(commands) != 4 || commands[0].(map[string]any)["command"] != "example" || commands[1].(map[string]any)["command"] != "projects" {
						t.Fatalf("invalid Telegram menu %#v", body)
					}
					return response(200, `{"ok":true,"result":true}`), nil
				}
				if r.URL.Host != "discord.com" || r.URL.Path != "/api/v10/applications/application/guilds/workspace/commands" || r.Method != "POST" {
					t.Fatal("must upsert only this named guild command")
				}
				if r.Header.Get("Authorization") != "Bot test-bot-token" || body["name"] != "example" {
					t.Fatal("missing bot auth or command")
				}
				options := body["options"].([]any)
				status := options[1].(map[string]any)
				argument := status["options"].([]any)[0].(map[string]any)
				if status["type"] != float64(1) || argument["type"] != float64(3) || argument["name"] != "project_id" || argument["required"] != true {
					t.Fatalf("invalid subcommand: %#v", status)
				}
				return response(200, `{"id":"command-id"}`), nil
			})
			if err = f.s.SyncCommands(context.Background(), c.ID, c.Revision); err != nil {
				t.Fatal(err)
			}
			current, err := f.s.load(c.ID)
			if err != nil {
				t.Fatal(err)
			}
			if provider == "telegram" && current.BotUsername != "ExampleBot" {
				t.Fatal("provider identity was not saved")
			}
			before := len(calls)
			if err = f.s.SyncCommands(context.Background(), c.ID, 0); !errors.Is(err, registry.ErrConflict) {
				t.Fatalf("stale sync: %v", err)
			}
			if len(calls) != before {
				t.Fatal("stale request contacted provider")
			}
			f.s.http.Transport = transportFunc(func(*http.Request) (*http.Response, error) { return response(403, `{"error":"test-bot-token"}`), nil })
			if err = f.s.SyncCommands(context.Background(), c.ID, current.Revision); err == nil || strings.Contains(err.Error(), "test-bot-token") {
				t.Fatalf("unsafe provider failure: %v", err)
			}
		})
	}
}

func TestNativeCommandsUseExistingEventPath(t *testing.T) {
	f := newFixture(t)
	cases := []struct{ provider, body, contentType string }{
		{"telegram", `{"update_id":1,"message":{"message_id":1,"chat":{"id":100},"from":{"id":200},"text":"/status@ExampleBot project_123"}}`, "application/json"},
		{"slack", "command=%2Fexample-status&text=project_123&channel_id=100&user_id=200&team_id=workspace&trigger_id=native-command", "application/x-www-form-urlencoded"},
		{"discord", `{"id":"native-command","type":2,"application_id":"application","guild_id":"workspace","channel_id":"100","member":{"user":{"id":"200"}},"data":{"name":"example","options":[{"type":1,"name":"status","options":[{"type":3,"name":"project_id","value":"project_123"}]}]}}`, "application/json"},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			c := savedConnection{Connection: Connection{ID: "fixture", Revision: 1, Spec: menuSpec(f, tc.provider)}, BotUsername: "ExampleBot"}
			request := &http.Request{Header: http.Header{"Content-Type": []string{tc.contentType}}}
			event, _, err := decodeEvent(c, request, []byte(tc.body))
			if err != nil || event == nil || !event.Command || event.Text != "status project_123" || event.ChannelID != "100" || event.UserID != "200" {
				t.Fatalf("normalization failed: %#v %v", event, err)
			}
			event.ChannelID = "foreign"
			if err = f.s.accept(c, *event); !errors.Is(err, errForbidden) {
				t.Fatalf("native commands must obey existing channel checks: %v", err)
			}
		})
	}
}

func TestCommandMenuRejectsUnknownAndForeignBotCommands(t *testing.T) {
	f := newFixture(t)
	c := savedConnection{Connection: Connection{Spec: menuSpec(f, "telegram")}, BotUsername: "ExampleBot"}
	for _, command := range []string{"/status@AnotherBot project", "/destroy project", "/projects_extra", "/example@AnotherBot status project"} {
		raw, _ := json.Marshal(map[string]any{"update_id": 1, "message": map[string]any{"chat": map[string]int{"id": 100}, "from": map[string]int{"id": 200}, "text": command}})
		e, _, err := decodeEvent(c, &http.Request{}, raw)
		if e != nil || err != nil {
			t.Fatalf("unrelated command must not reach assistant: %s", command)
		}
	}
	for _, raw := range []string{
		`[{"type":1,"name":"destroy"}]`,
		`[{"type":1,"name":"status","options":[{"type":3,"name":"project_id","value":123}]}]`,
		`[{"type":1,"name":"status","options":[{"type":3,"name":"wrong","value":"x"}]}]`,
		`[{"type":1,"name":"projects","options":[{"type":3,"name":"value","value":"x"}]}]`,
	} {
		var options []discordOption
		_ = json.Unmarshal([]byte(raw), &options)
		if _, err := discordCommandText(c, options); err == nil {
			t.Fatalf("accepted malformed options %s", raw)
		}
	}
	for _, text := range []string{"projects", "status p"} {
		options := []discordOption{{Name: "text", Type: 3, Value: json.RawMessage(`"` + text + `"`)}}
		if got, err := discordCommandText(c, options); err != nil || got != text {
			t.Fatal("legacy command registration broke")
		}
	}
}

func TestCommandDefinitionValidation(t *testing.T) {
	for _, commands := range [][]CommandDefinition{
		{{Name: "../x", Description: "invalid"}},
		{{Name: "ok", Description: "line\nbreak"}},
		{{Name: "ok", Description: "valid"}, {Name: "ok", Description: "duplicate"}},
		{{Name: "ok", Description: "valid", Argument: "project id"}},
		{{Name: "example", Description: "conflicts with root"}},
	} {
		if err := validateCommands(Spec{Command: "/example", Commands: commands}); err == nil {
			t.Fatalf("invalid catalog accepted %#v", commands)
		}
	}
}

func TestCommandMenuProviderFailureNeverChangesCredentials(t *testing.T) {
	f := newFixture(t)
	c, err := f.s.Save("", menuSpec(f, "telegram"))
	if err != nil {
		t.Fatal(err)
	}
	f.s.http.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		return response(200, `{"ok":false,"error":"test-bot-token"}`), nil
	})
	if err = f.s.SyncCommands(context.Background(), c.ID, c.Revision); err == nil {
		t.Fatal("false success")
	}
	saved, err := f.s.load(c.ID)
	if err != nil || saved.Secrets.BotToken != "test-bot-token" || saved.BotUsername != "" || saved.Revision != c.Revision {
		t.Fatal("failed sync changed the connection")
	}
}
