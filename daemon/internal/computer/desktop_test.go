package computer

import (
	"context"
	"errors"
	"io"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestDesktopUsesExistingPairedSSHTunnelAndRevokesOnlyItsDisplay(t *testing.T) {
	for _, websocket := range []bool{false, true} {
		t.Run(strconv.FormatBool(websocket), func(t *testing.T) {
			s, _ := fixture(t)
			var opened atomic.Int32
			s.SetDesktopViewer(8794, func(id string) error {
				if id != "view-one" && id != "view-two" {
					return errors.New("desktop session expired")
				}
				return nil
			}, func(ctx context.Context, id string, conn io.ReadWriteCloser) error {
				opened.Add(1)
				defer conn.Close()
				stop := context.AfterFunc(ctx, func() { conn.Close() })
				defer stop()
				_, err := io.Copy(conn, conn)
				return err
			})
			phone, id := approveForwardDevice(t, s, websocket)
			other, _ := approveForwardDevice(t, s, websocket)
			address := "127.0.0.1:8794"
			if conn, err := phone.Dial("tcp", address); err == nil {
				conn.Close()
				t.Fatal("display opened without its grant")
			}
			if _, err := s.openForward(ForwardRequest{ID: "unscoped", DeviceID: id, Port: 8794}); err == nil {
				t.Fatal("generic grant bypassed desktop authorization")
			}
			if _, err := s.openForward(ForwardRequest{ID: "expired-view", DeviceID: id, Port: 8794, DesktopSessionID: "expired"}); err == nil {
				t.Fatal("unknown desktop session accepted")
			}
			first := ForwardRequest{ID: "first-view", DeviceID: id, Port: 8794, DesktopSessionID: "view-one"}
			if _, err := s.openForward(first); err != nil {
				t.Fatal(err)
			}
			if _, err := s.openForward(first); err != nil {
				t.Fatal(err)
			}
			if len(s.forwards) != 1 {
				t.Fatal("grant renewal duplicated a desktop")
			}
			if conn, err := other.Dial("tcp", address); err == nil {
				conn.Close()
				t.Fatal("another phone borrowed the display grant")
			}
			display, err := phone.Dial("tcp", address)
			if err != nil {
				t.Fatal(err)
			}
			defer display.Close()
			if _, err := display.Write([]byte("frame")); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 5)
			if _, err := io.ReadFull(display, buf); err != nil || string(buf) != "frame" {
				t.Fatal("display transport", err)
			}
			if opened.Load() != 1 {
				t.Fatal("display handler was duplicated")
			}
			if _, err := s.openForward(ForwardRequest{ID: "replacement", DeviceID: id, Port: 8794, DesktopSessionID: "view-two"}); err != nil {
				t.Fatal(err)
			}
			closed := make(chan error, 1)
			go func() { _, err := display.Read(make([]byte, 1)); closed <- err }()
			select {
			case err := <-closed:
				if err == nil {
					t.Fatal("replaced display still active")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("display replacement hung")
			}
			if len(s.forwards) != 1 {
				t.Fatal("replacement accumulated grants")
			}
			// The same SSH client still opens the workspace API after video closes.
			api, err := phone.Dial("tcp", "127.0.0.1:8790")
			if err != nil {
				t.Fatal("display replacement closed the shared SSH connection", err)
			}
			api.Close()
		})
	}
}
