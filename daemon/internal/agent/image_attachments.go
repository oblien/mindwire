package agent

import (
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
)

// ImageAttachmentsVersion advertises image-only turns and durable image history.
const ImageAttachmentsVersion = 1

func HasUserInput(message string, options TurnOptions) bool {
	if strings.TrimSpace(message) != "" {
		return true
	}
	for _, attachment := range options.Attachments {
		if strings.TrimSpace(attachment.Path) != "" || len(attachment.Data) > 0 {
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
