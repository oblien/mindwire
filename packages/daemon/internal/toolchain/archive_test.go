package toolchain

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func zipFixture(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, body := range files {
		f, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(body))
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestNativeArchiveInstallsCompanionAndSurvivesRename(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	body := zipFixture(t, map[string]string{"runtime": "#!/bin/sh\ntest -x \"$NATIVE_HELPER\" || exit 1\n\"$NATIVE_HELPER\"\n", "helper": "#!/bin/sh\necho 1.4.3\n"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	defer server.Close()
	root := t.TempDir()
	stage := filepath.Join(root, "stage")
	os.Mkdir(stage, 0700)
	archive := Archive{URL: server.URL + "/native.zip", SHA256: fmt.Sprintf("%x", sha256.Sum256(body)), Executable: "runtime", Executables: []string{"helper"}, EnvironmentPaths: map[string]string{"NATIVE_HELPER": "helper"}}
	if err := installArchive(t.Context(), Spec{Name: "Native", Binary: "native"}, archive, stage); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(root, "selected")
	if err := os.Rename(stage, selected); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(filepath.Join(selected, "bin", "native")).Output()
	if err != nil || strings.TrimSpace(string(out)) != "1.4.3" {
		t.Fatalf("moved installation: %q %v", out, err)
	}
	archive.SHA256 = strings.Repeat("0", 64)
	stage = t.TempDir()
	if err := installArchive(t.Context(), Spec{Name: "Native", Binary: "native"}, archive, stage); err == nil {
		t.Fatal("checksum mismatch accepted")
	}
	if _, err := os.Stat(filepath.Join(stage, "dist")); !os.IsNotExist(err) {
		t.Fatal("unverified archive was extracted")
	}
}

func TestArchiveRejectsTraversalLinksAndSpecialFiles(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "C:\\escape", "nested/../../escape"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test.zip")
			os.WriteFile(path, zipFixture(t, map[string]string{name: "bad"}), 0600)
			if err := extractZip(path, t.TempDir()); err == nil {
				t.Fatal("unsafe archive path accepted")
			}
		})
	}
	for _, kind := range []byte{tar.TypeSymlink, tar.TypeLink, tar.TypeFifo} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			var b bytes.Buffer
			gz := gzip.NewWriter(&b)
			tw := tar.NewWriter(gz)
			tw.WriteHeader(&tar.Header{Name: "link", Typeflag: kind, Linkname: "/tmp/escape", Mode: 0700})
			tw.Close()
			gz.Close()
			path := filepath.Join(t.TempDir(), "test.tar.gz")
			os.WriteFile(path, b.Bytes(), 0600)
			if err := extractTar(path, t.TempDir()); err == nil {
				t.Fatal("archive link/special file accepted")
			}
		})
	}
}
