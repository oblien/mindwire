package surface

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/session"
)

// Opt-in, read-only measurement against the owner's already-enabled Mac.
// No input, screenshots, settings changes or credentials are emitted or saved.
func TestMacDesktopReadOnlyPerformance(t *testing.T) {
	p := performanceMac(t)
	defer p.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	measureMacDesktop(t, ctx, p)
}

func performanceMac(t *testing.T) *MacDesktop {
	t.Helper()
	directory := os.Getenv("MINDWIRE_MAC_PERFORMANCE_STATE_DIR")
	if directory == "" {
		t.Skip("requires an explicitly selected, enabled Mac desktop")
	}
	store, err := session.Open(filepath.Join(directory, "agent-state.json"))
	if err != nil {
		t.Fatal("could not read local desktop setup")
	}
	var saved savedLocalDesktop
	if json.Unmarshal([]byte(store.Get(localDesktopKey)), &saved) != nil || !saved.Enabled {
		t.Fatal("enable desktop access locally before measuring")
	}
	p, err := newMacDesktop(saved.LocalDesktopSettings)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func measureMacDesktop(t *testing.T, ctx context.Context, p *MacDesktop) {
	geometry, err := p.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var checks []time.Duration
	for range 10 {
		start := time.Now()
		if _, err := p.Connect(ctx); err != nil {
			t.Fatal(err)
		}
		checks = append(checks, time.Since(start))
	}
	sort.Slice(checks, func(i, j int) bool { return checks[i] < checks[j] })
	t.Logf("display=%dx%d geometry-check median=%s max=%s", geometry.Width, geometry.Height, checks[len(checks)/2], checks[len(checks)-1])

	// Match the compressed encoding used by Apple Screen Sharing without
	// retaining/decoding pixels. This isolates server/transport cost from UI work.
	conn, geometry, err := p.openViewer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(25 * time.Second))
	var encodings bytes.Buffer
	encodings.Write([]byte{2, 0, 0, 4})
	preferred := int32(6)
	if value, err := strconv.Atoi(os.Getenv("MINDWIRE_MAC_PERFORMANCE_ENCODING")); err == nil && value == 16 {
		preferred = 16
	}
	for _, encoding := range []int32{preferred, 0, -224, -223} {
		_ = binary.Write(&encodings, binary.BigEndian, encoding)
	}
	if _, err := conn.Write(encodings.Bytes()); err != nil {
		t.Fatal(err)
	}
	for frame := range 3 {
		start := time.Now()
		request := []byte{3, 0, 0, 0, 0, 0, 0, 0, 0, 0}
		binary.BigEndian.PutUint16(request[6:8], uint16(geometry.Width))
		binary.BigEndian.PutUint16(request[8:10], uint16(geometry.Height))
		if _, err := conn.Write(request); err != nil {
			t.Fatal(err)
		}
		var header [4]byte
		if _, err := io.ReadFull(conn, header[:]); err != nil || header[0] != 0 {
			t.Fatal("expected a framebuffer update", err)
		}
		count := int(binary.BigEndian.Uint16(header[2:]))
		wireBytes, rectangles := int64(4), 0
		used := map[int32]int{}
		for range count {
			var rect [12]byte
			if _, err := io.ReadFull(conn, rect[:]); err != nil {
				t.Fatal(err)
			}
			wireBytes += 12
			encoding := int32(binary.BigEndian.Uint32(rect[8:]))
			if encoding == -224 {
				break
			}
			if encoding == -223 {
				continue
			}
			var size int64
			switch encoding {
			case 6, 16:
				var length [4]byte
				if _, err := io.ReadFull(conn, length[:]); err != nil {
					t.Fatal(err)
				}
				wireBytes += 4
				size = int64(binary.BigEndian.Uint32(length[:]))
			case 0:
				size = int64(binary.BigEndian.Uint16(rect[4:])) * int64(binary.BigEndian.Uint16(rect[6:])) * 4
			default:
				t.Fatalf("unexpected encoding %d", encoding)
			}
			if size > 64<<20 {
				t.Fatal("frame exceeds measurement limit")
			}
			if _, err := io.CopyN(io.Discard, conn, size); err != nil {
				t.Fatal(err)
			}
			wireBytes += size
			rectangles++
			used[encoding]++
		}
		t.Logf("frame=%d receive=%s rectangles=%d wireBytes=%d encodings=%v", frame, time.Since(start), rectangles, wireBytes, used)
	}
}
