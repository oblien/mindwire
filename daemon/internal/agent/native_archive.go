package agent

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ArchiveTree includes every regular auxiliary file, including unknown future
// file types. Special files and links fail explicitly instead of disappearing.
func ArchiveTree(ctx context.Context, path, prefix string) ([]NativeFile, error) {
	var files []NativeFile
	err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if os.IsNotExist(err) && p == path {
			return nil
		}
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("native session contains an unsupported special file: %s", d.Name())
		}
		rel, err := filepath.Rel(path, p)
		if err != nil {
			return err
		}
		name := prefix
		if rel != "." {
			name += "/" + filepath.ToSlash(rel)
		}
		files = append(files, NativeFile{Name: name, Path: p, JSONLines: strings.HasSuffix(p, ".jsonl")})
		return nil
	})
	return files, err
}

func SafeArchiveName(name string) bool {
	return name != "" && name != "." && !strings.ContainsAny(name, "\\\x00") && !strings.HasPrefix(name, "/") && filepath.ToSlash(filepath.Clean(name)) == name && name != ".." && !strings.HasPrefix(name, "../")
}
