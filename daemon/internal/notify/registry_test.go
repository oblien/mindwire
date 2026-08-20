package notify

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// fakeStore satisfies notify.Store for factory tests.
type fakeStore struct{ url, channel, token string }

func (f fakeStore) NotifyConfig() (string, string, string) { return f.url, f.channel, f.token }

// recorder is a Notifier that records the notifications it received (and can be told to fail).
type recorder struct {
	got  []agent.Notification
	fail error
}

func (r *recorder) Notify(_ context.Context, n agent.Notification) error {
	r.got = append(r.got, n)
	return r.fail
}

// All instantiates only the ENABLED factories (ok==true), in registration order. A disabled factory is
// omitted so the fan-out never carries a no-op nil.
func TestRegisterAll(t *testing.T) {
	saved := factories
	t.Cleanup(func() { factories = saved })
	factories = nil

	a, b := &recorder{}, &recorder{}
	Register(func(Store) (Notifier, bool) { return a, true })
	Register(func(Store) (Notifier, bool) { return nil, false }) // disabled → skipped
	Register(func(Store) (Notifier, bool) { return b, true })

	got := All(fakeStore{})
	if len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("All should return the two enabled notifiers in order, got %v", got)
	}
}

// The built-in webhook self-registers (from webhook.go's init) and is always enabled — reading its
// target live means it is present even when currently unconfigured.
func TestWebhookSelfRegisters(t *testing.T) {
	found := false
	for _, n := range All(fakeStore{}) {
		if _, ok := n.(*WebhookNotifier); ok {
			found = true
		}
	}
	if !found {
		t.Fatal("webhook channel should self-register into the notify registry")
	}
}

// Fanout delivers to EVERY channel and, critically, one channel's failure never blocks the others; all
// errors are joined into the returned error.
func TestFanoutDeliversToAllDespiteFailure(t *testing.T) {
	a := &recorder{fail: errors.New("boom")}
	b := &recorder{}
	c := &recorder{}
	fan := Fanout{a, nil, b, c} // a nil channel is tolerated (skipped)

	n := agent.Notification{Condition: agent.Finished, Title: "t"}
	err := fan.Notify(context.Background(), n)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("fan-out should surface the failing channel's error, got %v", err)
	}
	// b and c still received it despite a failing first.
	for name, r := range map[string]*recorder{"b": b, "c": c} {
		if len(r.got) != 1 || r.got[0].Title != "t" {
			t.Errorf("channel %s should have received the notification, got %v", name, r.got)
		}
	}
}

// An empty fan-out is a silent no-op (no channels configured is the default).
func TestFanoutEmptyIsNoop(t *testing.T) {
	if err := Fanout(nil).Notify(context.Background(), agent.Notification{}); err != nil {
		t.Fatalf("empty fan-out should be a no-op, got %v", err)
	}
}
