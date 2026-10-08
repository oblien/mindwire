package codex

import "github.com/oblien/mindwire/daemon/internal/agent"

func (a appServer) inputText() string {
	files := a.files
	if a.nativeAudio {
		files = make([]agent.Attachment, 0, len(a.files))
		for _, file := range a.files {
			if agent.NativeAudioMime(file) == "" {
				files = append(files, file)
			}
		}
	}
	return a.message + agent.AttachmentReferenceText(files)
}

// Keep an extension on inline temporary files so Codex can identify the native media type.
func nativeAudioExtension(at agent.Attachment) string {
	switch agent.NativeAudioMime(at) {
	case "audio/wav":
		return ".wav"
	case "audio/mpeg":
		return ".mp3"
	case "audio/mp4":
		return ".m4a"
	case "audio/ogg":
		return ".ogg"
	case "audio/flac":
		return ".flac"
	case "audio/aac":
		return ".aac"
	case "audio/webm":
		return ".webm"
	}
	return ""
}
