package codex

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

type rolloutHistoryBase struct {
	ThreadID      string `json:"thread_id"`
	EndByteOffset int64  `json:"end_byte_offset"`
	EndOrdinal    int64  `json:"end_ordinal_exclusive"`
}

type rolloutSegment struct {
	path  string
	bytes int64 // 0 is the current file; ancestors always have a bounded prefix
}

// Modern Codex forks reference an immutable byte range in another native
// rollout. Follow those native references rather than copying or replaying text.
func rolloutSegments(path string) ([]rolloutSegment, string, error) {
	seen := map[string]bool{}
	stamp := sha256.New()
	var visit func(string, int64) ([]rolloutSegment, error)
	visit = func(path string, end int64) ([]rolloutSegment, error) {
		if seen[path] || len(seen) >= 64 {
			return nil, fmt.Errorf("Codex history has a cyclic or excessively deep fork")
		}
		seen[path] = true
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return nil, err
		}
		if end < 0 || end > info.Size() {
			return nil, fmt.Errorf("Codex fork's original history is incomplete")
		}
		if end > 0 {
			last := []byte{0}
			if _, err := file.ReadAt(last, end-1); err != nil || last[0] != '\n' {
				return nil, fmt.Errorf("invalid Codex fork history boundary")
			}
		}
		if end > 0 {
			fmt.Fprintf(stamp, "%s\x00%d\x00%d\x00%d\n", path, info.Size(), info.ModTime().UnixNano(), end)
		}
		base, err := readRolloutBase(file)
		if err != nil {
			return nil, err
		}
		var segments []rolloutSegment
		if base != nil {
			if !agent.ValidNativeSessionID(base.ThreadID) || base.EndByteOffset <= 0 {
				return nil, fmt.Errorf("this Codex fork history format needs a newer Mindwire service")
			}
			parent := findRollout(configBase(), base.ThreadID)
			if parent == "" {
				return nil, fmt.Errorf("Codex fork's original history is missing")
			}
			segments, err = visit(parent, base.EndByteOffset)
			if err != nil {
				return nil, err
			}
		}
		return append(segments, rolloutSegment{path, end}), nil
	}
	segments, err := visit(path, 0)
	return segments, hex.EncodeToString(stamp.Sum(nil)), err
}

func readRolloutBase(r io.Reader) (*rolloutHistoryBase, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 4096), 16<<20)
	if !sc.Scan() {
		return nil, sc.Err()
	}
	var header struct {
		Type    string `json:"type"`
		Payload struct {
			Base *rolloutHistoryBase `json:"history_base"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(sc.Bytes(), &header); err != nil {
		return nil, err
	}
	if header.Type != "session_meta" {
		return nil, nil
	}
	return header.Payload.Base, nil
}

func parseRolloutSegments(segments []rolloutSegment, leaf *os.File, chatID string) ([]agent.Message, error) {
	readers := make([]io.Reader, 0, len(segments))
	for i, segment := range segments {
		if i == len(segments)-1 {
			readers = append(readers, leaf)
			break
		}
		file, err := os.Open(segment.path)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		readers = append(readers, io.NewSectionReader(file, 0, segment.bytes))
	}
	return parseRollout(io.MultiReader(readers...), chatID)
}

// Native copy-on-write branches still own references to the parent's bytes.
// Deleting its app row can hide it, but must not break surviving branches.
func rolloutHasDependents(sid string) (bool, error) {
	for _, dir := range []string{"sessions", "archived_sessions"} {
		err := filepath.WalkDir(filepath.Join(configBase(), dir), func(path string, entry fs.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
				return nil
			}
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			base, err := readRolloutBase(file)
			file.Close()
			if err != nil {
				return err
			}
			if base != nil && base.ThreadID == sid {
				return errStopWalk
			}
			return nil
		})
		if err == errStopWalk {
			return true, nil
		}
		if err != nil {
			return false, err
		}
	}
	return false, nil
}
