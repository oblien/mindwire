package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"reflect"
)

// HistoryRevisionProvider opts native harnesses into conditional history reads.
// It must cover every transcript dependency, including native fork ancestors.
// An empty token or error asks the API to read history normally.
type HistoryRevisionProvider interface {
	HistoryRevision(HistoryQuery) (string, error)
}

// NativeHistoryFileRevision stats a file without parsing or hashing its contents.
// File identity detects replacement even when size and modification time match.
func NativeHistoryFileRevision(path string) (string, error) {
	if path == "" {
		return "missing", nil
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "missing", nil
	}
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	fmt.Fprintf(hash, "%s\x00%d\x00%d\x00%d", path, info.Size(), info.ModTime().UnixNano(), info.Mode())
	value := reflect.Indirect(reflect.ValueOf(info.Sys()))
	if value.IsValid() && value.Kind() == reflect.Struct {
		for _, name := range []string{"Dev", "Ino", "CreationTime", "FileIndexHigh", "FileIndexLow"} {
			if field := value.FieldByName(name); field.IsValid() && field.CanInterface() {
				fmt.Fprintf(hash, "\x00%s=%v", name, field.Interface())
			}
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
