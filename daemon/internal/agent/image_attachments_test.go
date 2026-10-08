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

func TestNativeAudioHistoryKeepsMediaOrderWithoutFetchingURLs(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"input_audio","audio_url":"data:audio/mp4;base64,AQID"},
		{"type":"input_image","image_url":"data:image/png;base64,BAUG"},
		{"type":"localAudio","path":"/private/voice.wav"},
		{"type":"audio","url":"https://example.invalid/voice.m4a"},
		{"type":"input_audio","audio_url":"data:image/png;base64,AQID"},
		{"type":"input_audio","audio_url":"data:audio/mp4;base64,not-base64"},
		{"type":"text","text":"data:audio/mp4;base64,AQID"}
	]`)
	attachments := InputAttachmentsFromContent(raw)
	if len(attachments) != 3 || attachments[0].Mime != "audio/mp4" || !bytes.Equal(attachments[0].Data, []byte{1, 2, 3}) ||
		attachments[1].Mime != "image/png" || attachments[2].Name != "voice.wav" || len(attachments[2].Data) != 0 {
		t.Fatalf("lost mixed input order or interpreted an unsupported block: %+v", attachments)
	}
	if images := ImagesFromContent(raw); len(images) != 1 || images[0].Mime != "image/png" {
		t.Fatal("image-only readers started interpreting audio")
	}
	recorded := []Attachment{{ArtifactID: "saved", Name: "Voice.m4a", Mime: "audio/mp4", Bytes: 3}}
	merged := MergeInputAttachments(attachments[:1], recorded)
	if len(merged) != 1 || merged[0].ArtifactID != "saved" || len(merged[0].Data) != 0 {
		t.Fatal("durable audio reference replaced by embedded native bytes")
	}
}
