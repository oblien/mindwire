package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// TestPushDeliveryLive is opt-in and targets only the physical iPhone which minted this short-lived
// send credential. PushDeliveryLiveTests on that phone checks actual native notification receipt.
func TestPushDeliveryLive(t *testing.T) {
	path := os.Getenv("MINDWIRE_LIVE_PUSH_FILE")
	if path == "" {
		t.Skip("requires the physical iPhone's notification test handoff")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config struct{ URL, Token, Channel, ChatID, RunPrefix string }
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if config.URL == "" || config.Token == "" || config.ChatID == "" || config.RunPrefix == "" {
		t.Fatal("incomplete handoff")
	}
	n := NewWebhook(func() (string, string, string) { return config.URL, config.Channel, config.Token })
	n.Format = func() agent.NotifyChannelType { return agent.ChannelPush }
	n.HTTP.Transport = deliveryCheckTransport{t: t}
	for _, check := range []struct {
		harness   string
		condition agent.Condition
	}{
		{"codex", agent.Finished}, {"claude-code", agent.WaitingApproval}, {"codex", agent.Errored},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := n.Notify(ctx, agent.Notification{
			Title: "Mindwire notification check", Body: "Test: " + string(check.condition),
			Condition: check.condition, Agent: check.harness, ChatID: config.ChatID,
			RunID: config.RunPrefix + "-" + string(check.condition),
		})
		cancel()
		if err != nil {
			t.Fatalf("%s: %v", check.condition, err)
		}
	}
}

type deliveryCheckTransport struct{ t *testing.T }

func (d deliveryCheckTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	response, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	response.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	var result struct {
		Devices, Delivered, Failed int
		Skipped                    bool
		Reason, Detail             string
	}
	if err := json.Unmarshal(body, &result); err != nil {
		d.t.Error("invalid push relay response")
	}
	d.t.Logf("HTTP %d: devices=%d delivered=%d failed=%d skipped=%v reason=%s detail=%s", response.StatusCode,
		result.Devices, result.Delivered, result.Failed, result.Skipped, result.Reason, result.Detail)
	if result.Devices != 1 || result.Delivered != 1 || result.Failed != 0 || result.Skipped {
		var details map[string]any
		if json.Unmarshal(body, &details) == nil {
			keys := make([]string, 0, len(details))
			for key := range details {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			d.t.Logf("relay response fields: %v; errors: %v", keys, pushErrorDetails(details))
		}
		d.t.Error("relay must deliver to exactly the iPhone running the check")
	}
	return response, nil
}

func pushErrorDetails(value any) []string {
	var out []string
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			key = strings.ToLower(key)
			if strings.Contains(key, "token") || strings.Contains(key, "secret") || strings.Contains(key, "authorization") {
				continue
			}
			if message, ok := child.(string); ok && (strings.Contains(key, "error") || key == "code" || key == "message" || key == "reason" || key == "detail") {
				message = regexp.MustCompile(`[A-Za-z0-9_:/.-]{80,}`).ReplaceAllString(message, "[redacted]")
				if len(message) > 400 {
					message = message[:400]
				}
				out = append(out, key+": "+message)
			} else {
				out = append(out, pushErrorDetails(child)...)
			}
		}
	case []any:
		for _, child := range value {
			out = append(out, pushErrorDetails(child)...)
		}
	}
	return out
}
