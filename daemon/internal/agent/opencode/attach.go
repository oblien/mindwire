package opencode

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// attach.go turns a turn's attachments into opencode prompt parts. Images ride opencode's native
// `file` part as a data: URL, which the server forwards to the model as TRUE vision input (its
// documented attachment mechanism — a live prompt_async with a file part returns HTTP 204, so the
// shape is honored). Non-image files can't use the vision channel, so they degrade to a path reference
// appended to the message text (the same posture codex/claude take for non-images). This is what makes
// Capabilities.ImageInput honest: the bytes reach the model, not just a path it must open.

// imageExts mirrors codex's isImage extension set: an attachment with no image mime is still treated as
// an image when its name/path carries one of these extensions.
var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true, ".svg": true,
}

// attachName is the best available name for extension/mime inference: the display name, else the path.
func attachName(at agent.Attachment) string {
	if at.Name != "" {
		return at.Name
	}
	return at.Path
}

// isImageAttachment reports whether an attachment should ride the vision `file` part (image mime, or a
// known image extension when the mime is absent).
func isImageAttachment(at agent.Attachment) bool {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(at.Mime)), "image/") {
		return true
	}
	return imageExts[strings.ToLower(filepath.Ext(attachName(at)))]
}

// imageMime resolves the MIME to stamp into the data: URL — the declared mime, else inferred from the
// extension, else a safe image/* default so the part is always well-formed.
func imageMime(at agent.Attachment) string {
	if m := strings.TrimSpace(at.Mime); m != "" {
		return m
	}
	switch strings.ToLower(filepath.Ext(attachName(at))) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".svg":
		return "image/svg+xml"
	default:
		return "image/png"
	}
}

// resolveAttachments converts attachments into extra prompt parts plus a message suffix. Images become
// `file` parts with a base64 data: URL (bytes come from Data, else read from Path); non-images are
// referenced by path in an appended "Attached files" block, with inline Data written to a per-turn temp
// file first so the reference resolves. The returned cleanup removes any such temp files and MUST be
// deferred by the caller — it is safe to call even when an error is returned.
func resolveAttachments(atts []agent.Attachment) (parts []map[string]any, msgAppend string, cleanup func(), err error) {
	var tmp agent.TempFiles
	cleanup = tmp.Cleanup
	if len(atts) == 0 {
		return nil, "", cleanup, nil
	}

	var refs []string
	for i, at := range atts {
		if isImageAttachment(at) {
			data := at.Data
			if len(data) == 0 && at.Path != "" {
				b, e := os.ReadFile(at.Path)
				if e != nil {
					return nil, "", cleanup, fmt.Errorf("read image attachment %d (%s): %w", i, at.Path, e)
				}
				data = b
			}
			if len(data) == 0 {
				continue // nothing to send
			}
			mime := imageMime(at)
			part := map[string]any{
				"type": "file",
				"mime": mime,
				"url":  "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data),
			}
			if n := strings.TrimSpace(at.Name); n != "" {
				part["filename"] = n
			}
			parts = append(parts, part)
			continue
		}

		// Non-image: reference a path, writing inline Data to a temp file first when there is no Path.
		path := at.Path
		if path == "" && len(at.Data) > 0 {
			p, e := tmp.Write("mindwire-opencode-attachment-*", at.Data)
			if e != nil {
				return nil, "", cleanup, fmt.Errorf("write attachment %d: %w", i, e)
			}
			path = p
		}
		if path == "" {
			continue
		}
		if n := strings.TrimSpace(at.Name); n != "" {
			refs = append(refs, fmt.Sprintf("- %s (%s)", n, path))
		} else {
			refs = append(refs, "- "+path)
		}
	}
	if len(refs) > 0 {
		msgAppend = "\n\nAttached files:\n" + strings.Join(refs, "\n")
	}
	return parts, msgAppend, cleanup, nil
}
