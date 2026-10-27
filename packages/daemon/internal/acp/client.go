// Package acp implements the Agent Client Protocol transport shared by native harnesses.
package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
)

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("ACP error %d: %s", e.Code, e.Message) }

type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   *RPCError       `json:"error"`
}

type Client struct {
	in            io.WriteCloser
	mu            sync.Mutex
	next          atomic.Int64
	pending       sync.Map // map[string]chan Message
	Notifications chan Message
	Requests      chan Message
	closed        chan struct{}
	stop          chan struct{}
	stopOnce      sync.Once
	readErr       error    // published before closed
	configOptions sync.Map // session ID -> []ConfigOption, including setup-time notifications
}

func rpcKey(id json.RawMessage) string { return string(bytes.TrimSpace(id)) }

func NewClient(in io.WriteCloser, out io.Reader) *Client {
	return NewClientWithOutput(in, out, nil)
}

// NewClientWithOutput permits a provider's login process to report a URL on a
// non-protocol stdout line. Payloads are not logged or persisted by the transport.
func NewClientWithOutput(in io.WriteCloser, out io.Reader, output func(string)) *Client {
	c := &Client{in: in, Notifications: make(chan Message, 128), Requests: make(chan Message, 16), closed: make(chan struct{}), stop: make(chan struct{})}
	go func() {
		defer close(c.closed)
		defer close(c.Notifications)
		defer close(c.Requests)
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
		for sc.Scan() {
			var m Message
			if json.Unmarshal(sc.Bytes(), &m) != nil {
				if output != nil {
					output(sc.Text())
				}
				continue
			}
			if len(m.ID) > 0 && (len(m.Result) > 0 || m.Error != nil) {
				if ch, ok := c.pending.LoadAndDelete(rpcKey(m.ID)); ok {
					ch.(chan Message) <- m
				}
				continue
			}
			if len(m.ID) > 0 && m.Method != "" {
				select {
				case c.Requests <- m:
				case <-c.stop:
					return
				}
				continue
			}
			if m.Method != "" {
				if m.Method == "session/update" {
					var update struct {
						SessionID string
						Update    struct {
							SessionUpdate string
							ConfigOptions []ConfigOption
						}
					}
					if json.Unmarshal(m.Params, &update) == nil && update.SessionID != "" && update.Update.SessionUpdate == "config_option_update" {
						c.configOptions.Store(update.SessionID, update.Update.ConfigOptions)
					}
				}
				select {
				case c.Notifications <- m:
				case <-c.stop:
					return
				}
			}
		}
		c.readErr = sc.Err()
	}()
	return c
}

// Session setup can replay an arbitrarily long transcript before replying.
// Consume that replay while awaiting the response, without treating it as output
// from the new prompt. The reader queues notifications before delivering the
// response, so draining the remaining queue establishes the prompt boundary.
func (c *Client) Setup(ctx context.Context, method string, params any) (json.RawMessage, error) {
	type response struct {
		raw json.RawMessage
		err error
	}
	done := make(chan response, 1)
	go func() {
		raw, err := c.Call(ctx, method, params)
		done <- response{raw, err}
	}()
	notifications, requests := c.Notifications, c.Requests
	Respond := func(req Message) error {
		if req.Method == "session/request_permission" {
			return c.Respond(req.ID, map[string]any{"outcome": map[string]string{"outcome": "cancelled"}})
		}
		return c.Reject(req.ID, -32601, "Client method unavailable during session setup")
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case result := <-done:
			for {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case _, ok := <-notifications:
					if !ok {
						notifications = nil
					}
				case req, ok := <-requests:
					if !ok {
						requests = nil
					} else if err := Respond(req); err != nil {
						return nil, err
					}
				default:
					return result.raw, result.err
				}
			}
		case _, ok := <-notifications:
			if !ok {
				notifications = nil
			}
		case req, ok := <-requests:
			if !ok {
				requests = nil
				continue
			}
			if err := Respond(req); err != nil {
				return nil, err
			}
		}
	}
}

func (c *Client) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.in.Write(append(b, '\n'))
	return err
}

func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return c.CallStarted(ctx, method, params, nil)
}

// CallStarted reports successful submission before the caller may send cancel.
// Native cancellation cannot overtake its prompt on the shared writer.
func (c *Client) CallStarted(ctx context.Context, method string, params any, started chan<- struct{}) (json.RawMessage, error) {
	id := c.next.Add(1)
	key := strconv.FormatInt(id, 10)
	ch := make(chan Message, 1)
	c.pending.Store(key, ch)
	defer c.pending.Delete(key)
	if err := c.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	if started != nil {
		close(started)
	}
	decode := func(m Message) (json.RawMessage, error) {
		if m.Error != nil {
			return nil, fmt.Errorf("ACP %s: %w", method, m.Error)
		}
		return m.Result, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case m := <-ch:
		return decode(m)
	case <-c.closed:
		// A response can be decoded immediately before stdout reaches EOF. Prefer
		// that queued reply over the close signal; otherwise a fast ACP process
		// flakes between its terminal response and process exit.
		select {
		case m := <-ch:
			return decode(m)
		default:
			if c.readErr != nil {
				return nil, fmt.Errorf("ACP stream: %w", c.readErr)
			}
			return nil, fmt.Errorf("ACP closed while waiting for %s", method)
		}
	}
}

func (c *Client) Respond(id json.RawMessage, result any) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "result": result})
}

func (c *Client) Reject(id json.RawMessage, code int, message string) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "id": id, "error": &RPCError{Code: code, Message: message}})
}

func (c *Client) Notify(method string, params any) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (c *Client) Close() {
	c.stopOnce.Do(func() { close(c.stop); _ = c.in.Close() })
}
