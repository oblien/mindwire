package agent

import (
	"reflect"
	"testing"
)

func TestQueuedHistoryMatchesNativeConsumptionAfterLaterSends(t *testing.T) {
	recorded := []Message{
		{ID: "initial", Role: "user", Text: "Again", CreatedAt: "2026-10-06T10:00:00Z"},
		{ID: "a1", Role: "assistant", Text: "First", CreatedAt: "2026-10-06T10:00:05Z"},
		{ID: "input-one", Role: "user", Text: "Again", CreatedAt: "2026-10-06T10:00:01Z"},
		{ID: "input-two", Role: "user", Text: "Again", CreatedAt: "2026-10-06T10:00:02Z"},
		{ID: "a2", Role: "assistant", Text: "Last", CreatedAt: "2026-10-06T10:00:08Z"},
	}
	native := append([]Message(nil), recorded...)
	native[0].ID = "native-initial"
	native[2].ID, native[2].CreatedAt = "native-one", "2026-10-06T10:00:06Z"
	native[3].ID, native[3].CreatedAt = "native-two", "2026-10-06T10:00:07Z"
	got := MergeRecordedHistory(native, recorded)
	if len(got) != len(native) || got[2].ID != "native-one" || got[3].ID != "native-two" {
		t.Fatalf("queued repeated prompts duplicated or misplaced: %+v", got)
	}
	if again := MergeRecordedHistory(got, recorded); !reflect.DeepEqual(again, got) {
		t.Fatalf("reloading duplicated history: %+v", again)
	}
}

func TestQueuedHistoryCombinesNativeInputBatchAndRetainsImages(t *testing.T) {
	image := Attachment{Name: "image.png", Mime: "image/png", Data: []byte("fixture")}
	recorded := []Message{
		{ID: "input-one", Role: "user", Text: "First", CreatedAt: "2026-10-06T10:00:00Z"},
		{ID: "input-two", Role: "user", Text: "Second", Attachments: []Attachment{image}, CreatedAt: "2026-10-06T10:00:01Z"},
		{ID: "reply", Role: "assistant", Text: "Done", CreatedAt: "2026-10-06T10:00:04Z"},
	}
	for _, text := range []string{"FirstSecond", "First\nSecond", "First\n\nSecond"} {
		native := []Message{{ID: "native-user", Role: "user", Text: text, CreatedAt: "2026-10-06T10:00:03Z"}, recorded[2]}
		got := MergeRecordedHistory(native, recorded)
		if len(got) != 2 || got[0].ID != "native-user" || !reflect.DeepEqual(got[0].Attachments, []Attachment{image}) {
			t.Fatalf("combined native input lost or duplicated: %+v", got)
		}
	}
	// An intervening response makes these separate turns, even if their
	// concatenation happens to occur in a later native prompt.
	separate := []Message{recorded[0], recorded[2], recorded[1]}
	if _, _, matched := queuedHistoryBatch(historyTurns(separate), 0, "FirstSecond"); matched {
		t.Fatal("batch matching crossed an assistant response")
	}
}
