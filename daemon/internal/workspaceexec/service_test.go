package workspaceexec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeFilesPathsAndResources(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	defer s.Terminals.Close()
	if err := os.Mkdir(filepath.Join(root, "folder"), 0700); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(root, "a '$ file.txt")
	if err := s.Write(name, "hello\nمرحبا 👋"); err != nil {
		t.Fatal(err)
	}
	content, err := s.Read(name)
	if err != nil || content.Content != "hello\nمرحبا 👋" {
		t.Fatalf("read: %#v %v", content, err)
	}
	items, err := s.Files(context.Background(), "", "")
	if err != nil || len(items) != 2 || items[0].Type != "dir" {
		t.Fatalf("files: %#v %v", items, err)
	}
	items, err = s.Files(context.Background(), "", "'$ file")
	if err != nil || len(items) != 1 || items[0].Path != filepath.ToSlash(name) {
		t.Fatalf("search: %#v %v", items, err)
	}
	home, _ := os.UserHomeDir()
	if actual, err := s.Path("~"); err != nil || actual != home {
		t.Fatalf("home: %s %v", actual, err)
	}
	if err := os.WriteFile(filepath.Join(root, "binary"), []byte{1, 0, 2}, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read("binary"); err == nil {
		t.Fatal("accepted binary text")
	}
	if err := s.Write("large", strings.Repeat("x", MaxFileBytes+1)); err == nil {
		t.Fatal("accepted oversized file")
	}
	if err := s.Delete("."); err == nil {
		t.Fatal("deleted workspace root")
	}
	if err := s.Delete(name); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatalf("file remained: %v", err)
	}
	resources, err := s.Resources(context.Background())
	if err != nil || resources.CPUs < 1 || resources.MemoryMB < 1 || resources.DiskMB < 1 {
		t.Fatalf("resources: %#v %v", resources, err)
	}
}
