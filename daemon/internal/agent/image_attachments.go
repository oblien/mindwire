package agent

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// ImageAttachmentsVersion advertises image-only turns and durable image history.
const ImageAttachmentsVersion = 1

// NativeImageMime is the common raster-image input understood by the harnesses.
// SVGs, PDFs, audio and other files remain ordinary readable attachments.
func NativeImageMime(at Attachment) string {
	media := strings.ToLower(strings.TrimSpace(at.Mime))
	switch media {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return media
	}
	for _, name := range []string{at.Name, at.Path} {
		switch strings.ToLower(filepath.Ext(name)) {
		case ".png":
			return "image/png"
		case ".jpg", ".jpeg":
			return "image/jpeg"
		case ".gif":
			return "image/gif"
		case ".webp":
			return "image/webp"
		}
	}
	return ""
}

func AttachmentReferenceText(attachments []Attachment) string {
	var refs []string
	for _, at := range attachments {
		if at.Path == "" || NativeImageMime(at) != "" {
			continue
		}
		if name := strings.TrimSpace(at.Name); name != "" {
			refs = append(refs, fmt.Sprintf("- %s (%s)", name, at.Path))
		} else {
			refs = append(refs, "- "+at.Path)
		}
	}
	if len(refs) == 0 {
		return ""
	}
	return "\n\nAttached files:\n" + strings.Join(refs, "\n")
}

func HasUserInput(message string, options TurnOptions) bool {
	if strings.TrimSpace(message) != "" {
		return true
	}
	for _, attachment := range options.Attachments {
		if attachment.ArtifactID != "" || strings.TrimSpace(attachment.Path) != "" || len(attachment.Data) > 0 {
			return true
		}
	}
	return false
}

// ImagesFromContent reads user input blocks only. Tool outputs remain artifacts,
// and remote URLs/local paths are never fetched while rendering history.
func ImagesFromContent(raw json.RawMessage) []Attachment {
	var blocks []json.RawMessage
	if json.Unmarshal(raw, &blocks) != nil {
		return nil
	}
	var images []Attachment
	for _, raw := range blocks {
		var block struct {
			Type     string          `json:"type"`
			Name     string          `json:"name"`
			Path     string          `json:"path"`
			URL      string          `json:"url"`
			ImageURL json.RawMessage `json:"image_url"`
			Source   struct {
				Type string `json:"type"`
				Mime string `json:"media_type"`
				Data []byte `json:"data"`
			} `json:"source"`
		}
		if json.Unmarshal(raw, &block) != nil {
			continue
		}
		switch block.Type {
		case "image", "input_image", "inputImage", "localImage", "local_image", "image_url":
		default:
			continue
		}
		image := Attachment{Name: block.Name, Path: block.Path}
		if block.Source.Type == "base64" && strings.HasPrefix(block.Source.Mime, "image/") {
			image.Mime, image.Data = block.Source.Mime, block.Source.Data
		} else {
			url := block.URL
			if len(block.ImageURL) > 0 {
				if json.Unmarshal(block.ImageURL, &url) != nil {
					var value struct {
						URL string `json:"url"`
					}
					_ = json.Unmarshal(block.ImageURL, &value)
					url = value.URL
				}
			}
			if decoded, ok := ImageFromDataURL(url); ok {
				image.Mime, image.Data = decoded.Mime, decoded.Data
			}
		}
		if len(image.Data) == 0 && image.Path == "" {
			continue
		}
		if image.Name == "" && image.Path != "" {
			image.Name = filepath.Base(image.Path)
		}
		images = append(images, image)
	}
	return images
}

func ImageFromDataURL(value string) (Attachment, bool) {
	header, body, ok := strings.Cut(value, ",")
	if !ok || !strings.HasPrefix(header, "data:image/") || !strings.HasSuffix(header, ";base64") || len(body) > 12<<20 {
		return Attachment{}, false
	}
	data, err := base64.StdEncoding.DecodeString(body)
	if err != nil || len(data) == 0 {
		return Attachment{}, false
	}
	return Attachment{Mime: strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64"), Data: data}, true
}

// MergeInputAttachments keeps native input order. Recorded bytes restore previews
// when a harness persists only a temporary image path which it later deletes.
func MergeInputAttachments(native, recorded []Attachment) []Attachment {
	// The native image block may embed megabytes of bytes and omit ordinary files.
	// A matched recorded turn retains the complete attachment order and small,
	// durable references; don't replace it with only the native image subset.
	for _, attachment := range recorded {
		if attachment.ArtifactID != "" {
			return recorded
		}
	}
	if len(native) == 0 {
		return recorded
	}
	if len(native) != len(recorded) {
		return native
	}
	result := append([]Attachment(nil), native...)
	for i := range result {
		if len(result[i].Data) == 0 {
			result[i].Data = recorded[i].Data
		}
		if result[i].Mime == "" {
			result[i].Mime = recorded[i].Mime
		}
		if result[i].Name == "" {
			result[i].Name = recorded[i].Name
		}
	}
	return result
}
