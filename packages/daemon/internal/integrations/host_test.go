package integrations

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHostDispatchVerifiedLeasesAndDelivery(t *testing.T) {
	for _, provider := range []string{"slack", "telegram", "discord", "teams"} {
		t.Run(provider, func(t *testing.T) {
			f := newFixture(t)
			spec := f.spec(provider)
			spec.BotID = ""
			spec.Dispatch = "host"
			c, err := f.s.Save("", spec)
			if err != nil {
				t.Fatal(err)
			}
			bad := f.incoming(t, c, "41", "projects", "200", "100")
			for key := range bad.Header {
				bad.Header.Del(key)
			}
			w := httptest.NewRecorder()
			f.s.Webhook(w, bad)
			if w.Code == 200 {
				t.Fatal("accepted unsigned event")
			}
			if f.post(t, c, "42", "projects", "200", "999") == 200 {
				t.Fatal("accepted unlisted channel")
			}
			if f.post(t, c, "43", "projects", "200", "100") != 200 {
				t.Fatal("rejected valid event")
			}
			first, err := f.s.Claim(c.ID)
			if err != nil || first == nil {
				t.Fatal(first, err)
			}
			encoded, _ := json.Marshal(first)
			var decoded map[string]any
			json.Unmarshal(encoded, &decoded)
			if decoded["interactionToken"] != nil {
				t.Fatal("leaked provider secret")
			}
			second, err := f.s.Claim(c.ID)
			if err != nil || second != nil {
				t.Fatal("claimed leased event", err)
			}
			if _, err = f.s.Complete(c.ID, first.ID, "wrong", "result"); err == nil {
				t.Fatal("accepted wrong lease")
			}
			if _, err = f.s.Complete(c.ID, first.ID, first.Lease, "result"); err != nil {
				t.Fatal(err)
			}
			if _, err = f.s.Complete(c.ID, first.ID, first.Lease, "result"); err != nil {
				t.Fatal("idempotent completion", err)
			}
			if _, err = f.s.Complete(c.ID, first.ID, first.Lease, "changed"); err == nil {
				t.Fatal("accepted different result")
			}
			f.s.Start()
			waitFor(t, func() bool { rows, _ := f.s.Events(c.ID); return len(rows) == 1 && rows[0].State == "delivered" })
			if f.runtime.count() != 0 {
				t.Fatal("host dispatch invoked AI")
			}
		})
	}
}
func TestHostExpiredLeaseAndConfigurationChange(t *testing.T) {
	f := newFixture(t)
	spec := f.spec("telegram")
	spec.Dispatch = "host"
	spec.BotID = ""
	c, err := f.s.Save("", spec)
	if err != nil {
		t.Fatal(err)
	}
	f.post(t, c, "11", "status", "200", "100")
	first, _ := f.s.Claim(c.ID)
	var e envelope
	f.db.SurfaceGet(eventKind, first.ID, &e)
	e.HostLeaseUntil = time.Now().Unix() - 1
	f.db.SurfacePut(eventKind, e.ID, e)
	if _, err = f.s.Complete(c.ID, first.ID, first.Lease, "late"); err == nil {
		t.Fatal("expired lease accepted")
	}
	next, _ := f.s.Claim(c.ID)
	if next == nil || next.Lease == first.Lease {
		t.Fatal("lease not replaced")
	}
	spec.ExpectedRevision = c.Revision
	spec.Enabled = false
	f.s.Save(c.ID, spec)
	if _, err = f.s.Complete(c.ID, next.ID, next.Lease, "stale"); err == nil {
		t.Fatal("disabled connection delivered")
	}
	if _, err = f.s.Claim(c.ID); err == nil {
		t.Fatal("disabled connection claimed")
	}
}

func TestHostClaimRetiresDisallowedQueueEntries(t *testing.T) {
	f := newFixture(t)
	spec := f.spec("telegram")
	spec.Dispatch, spec.BotID = "host", ""
	c, err := f.s.Save("", spec)
	if err != nil {
		t.Fatal(err)
	}
	if f.post(t, c, "11", "projects", "200", "100") != 200 {
		t.Fatal("rejected valid event")
	}
	spec.ExpectedRevision = c.Revision
	spec.ChannelIDs = []string{"101"}
	if _, err = f.s.Save(c.ID, spec); err != nil {
		t.Fatal(err)
	}
	if event, err := f.s.Claim(c.ID); err != nil || event != nil {
		t.Fatal("claimed an event from a removed channel", event, err)
	}
	rows, err := f.s.Events(c.ID)
	if err != nil || len(rows) != 1 || rows[0].State != "failed" {
		t.Fatal("disallowed event still occupies the queue", rows, err)
	}
	if f.runtime.count() != 0 {
		t.Fatal("host dispatch invoked AI")
	}
}
