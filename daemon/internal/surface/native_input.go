package surface

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
)

// execInput uses the runtime's streamed task/input contract. Data goes over stdin,
// never into command arguments, process listings, task logs or shell interpolation.
func (p *Oblien) execInput(ctx context.Context, command []string, text string) (int, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	const marker = "MINDWIRE_NATIVE_INPUT_READY"
	script := fmt.Sprintf("stty -echo -icanon 2>/dev/null || true\nprintf '%s\\n'\n/usr/bin/head -c %d | /usr/bin/base64 -D | \"$@\"", marker, len(encoded))
	cmd := append([]string{"/bin/sh", "-c", script, "mindwire-input"}, command...)
	path := "/runtimes/" + url.PathEscape(p.target) + "/exec"
	body, _ := json.Marshal(map[string]any{"cmd": cmd, "keep_logs": false, "timeout_seconds": 10, "ttl_seconds": 25})
	response, err := p.request(ctx, "POST", path+"/stream", body, "application/json")
	if err != nil {
		return -1, "", err
	}
	defer response.Body.Close()
	taskID := ""
	sent := false
	stdout := ""
	exit := -1
	exited := false
	defer func() {
		if taskID != "" {
			c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = p.json(c, "DELETE", path+"/"+url.PathEscape(taskID), nil, nil)
		}
	}()
	reader := bufio.NewScanner(io.LimitReader(response.Body, 2<<20))
	reader.Buffer(make([]byte, 4096), 1<<20)
	event := ""
	for reader.Scan() {
		line := reader.Text()
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var payload struct {
			TaskID   string `json:"task_id"`
			Data     string `json:"data"`
			ExitCode *int   `json:"exit_code"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &payload) != nil {
			continue
		}
		switch event {
		case "task_id":
			taskID = payload.TaskID
		case "stdout":
			data, e := base64.StdEncoding.DecodeString(payload.Data)
			if e != nil {
				return -1, "", problem("invalid_response", "Invalid native desktop task output.")
			}
			stdout += string(data)
			if len(stdout) > 65536 {
				return -1, "", problem("output_limit", "Native desktop task returned too much output.")
			}
		case "exit":
			if payload.ExitCode != nil {
				exit = *payload.ExitCode
				exited = true
			}
		}
		if taskID != "" && !sent && strings.Contains(stdout, marker) {
			sent = true
			stdout = strings.Replace(stdout, marker, "", 1)
			if encoded != "" {
				r, e := p.request(ctx, "POST", path+"/"+url.PathEscape(taskID)+"/input", []byte(encoded), "text/plain")
				if e != nil {
					return -1, "", e
				}
				io.Copy(io.Discard, io.LimitReader(r.Body, 65536))
				r.Body.Close()
			}
		}
	}
	if err := reader.Err(); err != nil {
		return -1, "", err
	}
	if !sent || !exited {
		return -1, "", problem("clipboard_failed", "The native desktop input task did not complete.")
	}
	return exit, strings.TrimSpace(stdout), nil
}
