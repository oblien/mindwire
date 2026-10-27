package toolchain

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Archive describes a vendor's complete distribution, including companion
// executables. Paths are relative to the archive root. No install scripts run.
type Archive struct {
	URL              string            `json:"url"`
	SHA256           string            `json:"sha256,omitempty"`
	Executable       string            `json:"executable"`
	Executables      []string          `json:"executables,omitempty"`
	EnvironmentPaths map[string]string `json:"environmentPaths,omitempty"`
}

func installDistribution(ctx context.Context, spec Spec, version, prefix string) error {
	if spec.Archives == nil {
		return installNPM(ctx, spec, version, prefix)
	}
	archive, ok := spec.Archives[version][runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return fmt.Errorf("%s %s has no bundled installer for %s/%s", spec.Name, version, runtime.GOOS, runtime.GOARCH)
	}
	return installArchive(ctx, spec, archive, prefix)
}

const maxArchiveBytes = 1 << 30
const maxExtractedBytes = 2 << 30

func installArchive(ctx context.Context, spec Spec, archive Archive, prefix string) error {
	reportInstallProgress(ctx, "downloading")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archive.URL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", spec.Name, resp.Status)
	}
	f, err := os.CreateTemp(prefix, ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxArchiveBytes+1))
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if n > maxArchiveBytes {
		return fmt.Errorf("%s archive exceeds size limit", spec.Name)
	}
	if archive.SHA256 != "" && !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), archive.SHA256) {
		return fmt.Errorf("%s archive checksum mismatch", spec.Name)
	}
	dist := filepath.Join(prefix, "dist")
	if err := os.MkdirAll(dist, 0700); err != nil {
		return err
	}
	if strings.HasSuffix(archive.URL, ".zip") {
		err = extractZip(f.Name(), dist)
	} else {
		err = extractTar(f.Name(), dist)
	}
	if err != nil {
		return err
	}
	for _, path := range append([]string{archive.Executable}, archive.Executables...) {
		full, err := archivePath(dist, path)
		if err != nil {
			return err
		}
		info, err := os.Stat(full)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("archive executable %s is not a regular file", path)
		}
		if err := os.Chmod(full, 0700); err != nil {
			return err
		}
	}
	return archiveLauncher(prefix, spec.Binary, archive)
}

func archivePath(root, name string) (string, error) {
	// Reject Windows separators/drives on every platform as archives are portable.
	if name == "" || strings.ContainsAny(name, "\\:\x00") || filepath.IsAbs(name) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	clean := filepath.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	return filepath.Join(root, clean), nil
}

func writeArchiveFile(root, name string, mode os.FileMode, src io.Reader, size int64, remaining *int64) error {
	path, err := archivePath(root, name)
	if err != nil {
		return err
	}
	if mode.IsDir() {
		return os.MkdirAll(path, 0700)
	}
	if !mode.IsRegular() {
		return fmt.Errorf("archive links and special files are unsupported: %s", name)
	}
	if size < 0 || size > *remaining {
		return fmt.Errorf("archive exceeds extracted size limit")
	}
	*remaining -= size
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600|mode.Perm()&0100)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(src, size+1))
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if n != size {
		return fmt.Errorf("archive entry %s has incorrect size", name)
	}
	return nil
}

func extractZip(path, root string) error {
	z, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer z.Close()
	remaining := int64(maxExtractedBytes)
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			return err
		}
		err = writeArchiveFile(root, f.Name, f.Mode(), r, int64(f.UncompressedSize64), &remaining)
		r.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func extractTar(path, root string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	r := tar.NewReader(gz)
	remaining := int64(maxExtractedBytes)
	for {
		h, err := r.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeDir && h.Typeflag != tar.TypeReg {
			return fmt.Errorf("archive links and special files are unsupported: %s", h.Name)
		}
		if err := writeArchiveFile(root, h.Name, h.FileInfo().Mode(), r, h.Size, &remaining); err != nil {
			return err
		}
	}
}

func archiveLauncher(prefix, binary string, archive Archive) error {
	if _, err := archivePath(".", binary); err != nil {
		return err
	}
	for key, path := range archive.EnvironmentPaths {
		if !validEnvKey(key) {
			return fmt.Errorf("invalid archive environment key")
		}
		if _, err := archivePath(".", path); err != nil {
			return err
		}
	}
	if runtime.GOOS == "windows" {
		body := "@echo off\r\nsetlocal\r\n"
		for k, v := range archive.EnvironmentPaths {
			body += "set \"" + k + "=%~dp0dist\\" + filepath.FromSlash(v) + "\"\r\n"
		}
		body += "\"%~dp0dist\\" + filepath.FromSlash(archive.Executable) + "\" %*\r\n"
		return os.WriteFile(filepath.Join(prefix, binary+".cmd"), []byte(body), 0600)
	}
	if err := os.MkdirAll(filepath.Join(prefix, "bin"), 0700); err != nil {
		return err
	}
	body := "#!/bin/sh\nset -eu\nmindwire_distribution=$(CDPATH= cd -- \"$(dirname -- \"$0\")/../dist\" && pwd)\n"
	for k, v := range archive.EnvironmentPaths {
		body += "export " + k + "=\"$mindwire_distribution\"/" + quote(v) + "\n"
	}
	body += "exec \"$mindwire_distribution\"/" + quote(archive.Executable) + " \"$@\"\n"
	return os.WriteFile(filepath.Join(prefix, "bin", binary), []byte(body), 0700)
}
