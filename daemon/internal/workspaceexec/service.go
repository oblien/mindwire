// Package workspaceexec implements the native execution surface of a workspace.
// Transport and UI clients do not need OS-specific file or resource shell scripts.
package workspaceexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/oblien/mindwire/daemon/internal/proc"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
)

const Version = 1
const MaxFileBytes = 4 << 20

type Service struct {
	Root          string
	jobs          chan struct{}
	Terminals     *Terminals
	gate          *sync.Mutex
	checkMutation func(string) error
	activityMu    sync.Mutex
	activePaths   map[string]int
}

func New(root string) *Service {
	if root == "" {
		root, _ = os.Getwd()
	}
	root, _ = filepath.Abs(root)
	s := &Service{Root: root, jobs: make(chan struct{}, 8)}
	s.Terminals = NewTerminals(s)
	return s
}

type Info struct {
	Hostname  string `json:"hostname"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Home      string `json:"home"`
	Directory string `json:"directory"`
	PID       int    `json:"pid"`
}

func (s *Service) Info() Info {
	host, _ := os.Hostname()
	home, _ := os.UserHomeDir()
	return Info{host, runtime.GOOS, runtime.GOARCH, filepath.ToSlash(home), filepath.ToSlash(s.Root), os.Getpid()}
}

func (s *Service) Path(path string) (string, error) {
	if strings.ContainsRune(path, '\x00') {
		return "", errors.New("invalid path")
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, path[2:])
		}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.Root, path)
	}
	return filepath.Abs(path)
}

type Resources struct {
	CPUs     int    `json:"cpus"`
	MemoryMB uint64 `json:"memoryMb"`
	DiskMB   uint64 `json:"diskMb"`
}

func (s *Service) Resources(ctx context.Context) (Resources, error) {
	memory, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return Resources{}, err
	}
	storage, err := disk.UsageWithContext(ctx, s.Root)
	if err != nil {
		return Resources{}, err
	}
	return Resources{runtime.NumCPU(), memory.Total >> 20, storage.Total >> 20}, nil
}

type File struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Type       string `json:"type"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modifiedAt"`
	Ignored    bool   `json:"isIgnored"`
}

func fileEntry(path string, info fs.FileInfo) File {
	kind := "file"
	if info.IsDir() {
		kind = "dir"
	}
	return File{info.Name(), filepath.ToSlash(path), kind, info.Size(), info.ModTime().UTC().Format(time.RFC3339Nano), false}
}

func (s *Service) Files(ctx context.Context, path, query string) ([]File, error) {
	root, err := s.Path(path)
	if err != nil {
		return nil, err
	}
	items := []File{}
	if query != "" {
		query = strings.ToLower(query)
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if len(items) >= 1000 {
				return fs.SkipAll
			}
			if err != nil {
				if path == root {
					return err
				}
				return nil
			}
			if entry.Name() == ".git" {
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if path != root && strings.Contains(strings.ToLower(entry.Name()), query) {
				if info, err := entry.Info(); err == nil {
					items = append(items, fileEntry(path, info))
				}
			}
			return nil
		})
	} else {
		var entries []os.DirEntry
		entries, err = os.ReadDir(root)
		if err == nil {
			for _, entry := range entries {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				if entry.Name() == ".git" {
					continue
				}
				path := filepath.Join(root, entry.Name())
				if info, e := os.Stat(path); e == nil {
					items = append(items, fileEntry(path, info))
				}
			}
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Type != items[j].Type {
			return items[i].Type == "dir"
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	return items, err
}

type Content struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Size    int    `json:"size"`
}

func (s *Service) Read(path string) (Content, error) {
	path, err := s.Path(path)
	if err != nil {
		return Content{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return Content{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Content{}, err
	}
	if !info.Mode().IsRegular() {
		return Content{}, errors.New("this path is not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return Content{}, err
	}
	if len(data) > MaxFileBytes {
		return Content{}, errors.New("this file is too large to open as text")
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return Content{}, errors.New("this is not a text file")
	}
	return Content{filepath.ToSlash(path), string(data), len(data)}, nil
}

func (s *Service) Write(path, content string) error {
	path, err := s.Path(path)
	if err != nil {
		return err
	}
	if len(content) > MaxFileBytes {
		return errors.New("file exceeds the text size limit")
	}
	if resolved, e := filepath.EvalSymlinks(path); e == nil {
		path = resolved
	} else if !os.IsNotExist(e) {
		return e
	}
	release, err := s.beginMutation(path)
	if err != nil {
		return err
	}
	defer release()
	mode := fs.FileMode(0600)
	if info, e := os.Stat(path); e == nil {
		if !info.Mode().IsRegular() {
			return errors.New("this path is not a regular file")
		}
		mode = info.Mode().Perm()
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".mindwire-write-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.WriteString(content)
	}
	if err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (s *Service) Delete(path string) error {
	path, err := s.Path(path)
	if err != nil {
		return err
	}
	if path == filepath.Dir(path) || path == s.Root {
		return errors.New("cannot delete the workspace root")
	}
	release, err := s.beginMutation(path)
	if err != nil {
		return err
	}
	defer release()
	return os.RemoveAll(path)
}

type ExecRequest struct {
	Argv      []string `json:"argv"`
	Directory string   `json:"directory,omitempty"`
	Input     string   `json:"input,omitempty"`
	TimeoutMS int      `json:"timeoutMs,omitempty"`
}
type ExecResult struct {
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	ExitCode  int    `json:"exitCode"`
	Truncated bool   `json:"truncated,omitempty"`
}

func Executable(name string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	// Git for Windows supplies the POSIX shell used by existing Git scripts.
	if runtime.GOOS == "windows" && (name == "sh" || name == "bash") {
		if git, err := exec.LookPath("git"); err == nil {
			path := filepath.Join(filepath.Dir(git), "..", "bin", name+".exe")
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("%s is not installed on this computer", name)
}

func (s *Service) Exec(ctx context.Context, req ExecRequest, output func(string, []byte)) (ExecResult, error) {
	if len(req.Argv) == 0 || len(req.Argv) > 256 {
		return ExecResult{}, errors.New("a command is required")
	}
	select {
	case s.jobs <- struct{}{}:
		defer func() { <-s.jobs }()
	case <-ctx.Done():
		return ExecResult{}, ctx.Err()
	}
	duration := 300 * time.Second
	if req.TimeoutMS > 0 {
		duration = min(time.Duration(req.TimeoutMS)*time.Millisecond, 10*time.Minute)
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	path, err := Executable(req.Argv[0])
	if err != nil {
		return ExecResult{}, err
	}
	directory, err := s.Path(req.Directory)
	if err != nil {
		return ExecResult{}, err
	}
	release, err := s.beginMutation(directory)
	if err != nil {
		return ExecResult{}, err
	}
	defer release()
	cmd := exec.CommandContext(ctx, path, req.Argv[1:]...)
	proc.Group(cmd)
	cmd.Dir = directory
	cmd.Stdin = strings.NewReader(req.Input)
	stdout, stderr := &boundedOutput{kind: "stdout", emit: output}, &boundedOutput{kind: "stderr", emit: output}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err = cmd.Run()
	result := ExecResult{stdout.String(), stderr.String(), 0, stdout.truncated || stderr.truncated}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return result, err
	}
	return result, nil
}

type boundedOutput struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	kind      string
	emit      func(string, []byte)
	truncated bool
}

func (w *boundedOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	if w.emit != nil {
		w.emit(w.kind, p)
	}
	remaining := MaxFileBytes - w.buf.Len()
	if len(p) > remaining {
		w.truncated = true
		p = p[:remaining]
	}
	_, _ = w.buf.Write(p)
	return n, nil
}
func (w *boundedOutput) String() string { w.mu.Lock(); defer w.mu.Unlock(); return w.buf.String() }
