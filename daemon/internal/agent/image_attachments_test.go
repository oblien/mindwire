package agent

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestUserImageBlocksAndRecordedTemporaryFiles(t *testing.T) {
	for _, raw := range []string{
		`[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AQID"}}]`,
		`[{"type":"input_image","image_url":"data:image/png;base64,AQID"}]`,
		`[{"type":"image","url":"data:image/png;base64,AQID"}]`,
	} {
		images := ImagesFromContent(json.RawMessage(raw))
		if len(images) != 1 || images[0].Mime != "image/png" || !bytes.Equal(images[0].Data, []byte{1, 2, 3}) {
			t.Fatalf("Image input lost: %+v", images)
		}
	}
	if images := ImagesFromContent(json.RawMessage(`[{"type":"text","text":"data:image/png;base64,AQID"}]`)); len(images) != 0 {
		t.Fatal("Text became an image")
	}
	native := []Message{{ID: "native", Role: "user", Text: "", CreatedAt: "2026-09-25T10:00:00Z", Attachments: []Attachment{{Path: "/tmp/deleted.png"}}}}
	recorded := []Message{{ID: "recorded", Role: "user", Text: "", CreatedAt: native[0].CreatedAt, Attachments: []Attachment{{Name: "photo.png", Mime: "image/png", Data: []byte{1, 2, 3}}}}}
	merged := MergeRecordedHistory(native, recorded)
	if len(merged) != 1 || !bytes.Equal(merged[0].Attachments[0].Data, recorded[0].Attachments[0].Data) {
		t.Fatal("Temporary image preview lost")
	}
	if len(native[0].Attachments[0].Data) != 0 {
		t.Fatal("Merge mutated the native cache")
	}
	clone := cloneHistory(merged)
	clone[0].Attachments[0].Data[0] = 9
	if merged[0].Attachments[0].Data[0] != 1 {
		t.Fatal("Image bytes shared across history cache readers")
	}
}
