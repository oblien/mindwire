package surface

import (
	"context"
	"image"
	"math/bits"
	"time"
)

// One explicit observation shares the controller connection with a pending
// incremental 1px request for cursor changes. A cursor-only reply cannot satisfy
// a screenshot or geometry check. Coverage also handles tiled framebuffer replies.
type frameObservation struct {
	full      bool
	size      Geometry
	sent      bool
	remaining int
	covered   []uint64
}

func (o *frameObservation) reset(size Geometry) {
	o.size = size
	o.sent = false
	o.remaining = 1
	if o.full {
		o.remaining = size.Width * size.Height
	}
	o.covered = make([]uint64, (o.remaining+63)/64)
}

func (o *frameObservation) cover(rect image.Rectangle) {
	if !o.sent || o.remaining == 0 {
		return
	}
	width, height := 1, 1
	if o.full {
		width, height = o.size.Width, o.size.Height
	}
	rect = rect.Intersect(image.Rect(0, 0, width, height))
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		start, end := y*width+rect.Min.X, y*width+rect.Max.X
		for start < end {
			word, offset := start/64, start%64
			n := min(64-offset, end-start)
			mask := (^uint64(0) >> uint(64-n)) << uint(offset)
			o.remaining -= bits.OnesCount64(mask &^ o.covered[word])
			o.covered[word] |= mask
			start += n
		}
	}
}

func (r *rfbClient) wakeUpdates() {
	select {
	case r.updateWake <- struct{}{}:
	default:
	}
}

// A pending incremental request sleeps at the VNC server until its cursor (or
// that single pixel) changes. No idle polling, full second video stream, or extra
// pointer events. Explicit captures take priority and wake a held request.
func (r *rfbClient) requestUpdates() {
	var nextCursorAt time.Time
	for {
		select {
		case <-r.done:
			return
		case <-r.updateWake:
		}
		r.mu.Lock()
		o := r.observation
		watch := o == nil && r.onCursor != nil && !r.cursorPending
		if watch && time.Now().Before(nextCursorAt) {
			r.mu.Unlock()
			timer := time.NewTimer(time.Until(nextCursorAt))
			select {
			case <-r.done:
				timer.Stop()
				return
			case <-r.updateWake:
				timer.Stop()
			case <-timer.C:
			}
			r.wakeUpdates()
			continue
		}
		width, height := uint16(1), uint16(1)
		send := watch
		if o != nil && !o.sent {
			o.sent = true
			send = true
			if o.full {
				width, height = uint16(o.size.Width), uint16(o.size.Height)
			}
		}
		if watch {
			r.cursorPending = true
			nextCursorAt = time.Now().Add(50 * time.Millisecond)
		}
		r.mu.Unlock()
		if send {
			err := r.withWrite(context.Background(), func() error {
				return r.client.FramebufferUpdateRequest(watch, 0, 0, width, height)
			})
			if err != nil {
				r.close()
				return
			}
		}
	}
}
