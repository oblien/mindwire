package mindwire

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/oblien/mindwire/daemon/internal/artifact"
)

type AttachmentUploadChunk = artifact.UploadChunk
type AttachmentUploadState = artifact.UploadState

// UploadAttachmentChunk mirrors the resumable authenticated HTTP upload. A retry
// returns the verified offset, without duplicating bytes or overwriting a file.
func (c *Client) UploadAttachmentChunk(chatID, id string, chunk AttachmentUploadChunk) (AttachmentUploadState, error) {
	c.core.registryMu.Lock()
	defer c.core.registryMu.Unlock()
	return c.core.surfaces.Artifacts().Upload(chatID, id, chunk)
}

// UploadAttachment stores a file beside the conversation and returns a small
// reference for Turn or InputWithOptions. The native CLI receives image input or
// a durable file path, and the data travels with project context switching.
func (c *Client) UploadAttachment(chatID, name, mime string, data []byte) (Attachment, error) {
	hash := sha256.Sum256(data)
	digest := hex.EncodeToString(hash[:])
	identity := sha256.Sum256([]byte(chatID + "\x00" + name + "\x00" + digest))
	id := hex.EncodeToString(identity[:16])
	if mime == "" {
		mime = "application/octet-stream"
	}
	for offset := 0; ; {
		end := min(len(data), offset+artifact.MaxUploadChunk)
		state, err := c.UploadAttachmentChunk(chatID, id, AttachmentUploadChunk{
			Name: name, Mime: mime, Bytes: len(data), SHA256: digest, Offset: offset, Data: data[offset:end],
		})
		if err != nil {
			return Attachment{}, err
		}
		if state.Complete {
			return Attachment{ArtifactID: id, Name: name, Mime: mime, Bytes: len(data)}, nil
		}
		offset = state.Offset
	}
}
