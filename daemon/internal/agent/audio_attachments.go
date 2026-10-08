package agent

import (
	"path/filepath"
	"strings"
)

// NativeAudioMime identifies the formats accepted by Codex localAudio. Other
// audio remains an ordinary file; adapters still decide whether the model hears it.
func NativeAudioMime(at Attachment) string {
	media := strings.ToLower(strings.TrimSpace(at.Mime))
	switch media {
	case "audio/wav", "audio/wave", "audio/x-wav":
		return "audio/wav"
	case "audio/mpeg", "audio/mp3":
		return "audio/mpeg"
	case "audio/mp4", "audio/m4a", "audio/x-m4a":
		return "audio/mp4"
	case "audio/ogg", "audio/opus":
		return "audio/ogg"
	case "audio/flac", "audio/x-flac":
		return "audio/flac"
	case "audio/aac", "audio/webm":
		return media
	}
	for _, name := range []string{at.Name, at.Path} {
		switch strings.ToLower(filepath.Ext(name)) {
		case ".wav":
			return "audio/wav"
		case ".mp3":
			return "audio/mpeg"
		case ".m4a":
			return "audio/mp4"
		case ".ogg", ".opus":
			return "audio/ogg"
		case ".flac":
			return "audio/flac"
		case ".aac":
			return "audio/aac"
		case ".webm":
			return "audio/webm"
		}
	}
	return ""
}
