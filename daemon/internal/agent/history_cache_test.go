package agent

import (
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNativeHistoryCacheSharesReadsAndInvalidatesOnNativeChanges(t *testing.T) {
	cache := newHistoryCache(4, 1<<20)
	path := filepath.Join(t.TempDir(), "native.jsonl")
	if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	var parses atomic.Int32
	parse := func(f *os.File) ([]Message, error) {
		parses.Add(1)
		time.Sleep(time.Millisecond)
		data, err := io.ReadAll(f)
		return []Message{{ID: "native", Text: string(data)}}, err
	}
	var group sync.WaitGroup
	for range 12 {
		group.Add(1)
		go func() {
			defer group.Done()
			messages, err := cache.Read(path, "chat", parse)
			if err != nil || len(messages) != 1 || messages[0].Text != "first" {
				t.Error("incorrect shared read")
			}
		}()
	}
	group.Wait()
	if parses.Load() != 1 {
		t.Fatalf("parsed %d times", parses.Load())
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteString(" continued outside Mindwire")
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	continued, err := cache.Read(path, "chat", parse)
	if err != nil || continued[0].Text != "first continued outside Mindwire" || parses.Load() != 2 {
		t.Fatal("native append was hidden by cache")
	}
	// Same length and timestamp, new inode: a native compaction/replacement must invalidate.
	stamp, _ := os.Stat(path)
	replacement := path + ".new"
	if err := os.WriteFile(replacement, []byte("other continued outside Mindwire"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(replacement, stamp.ModTime(), stamp.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	rewritten, err := cache.Read(path, "chat", parse)
	if err != nil || rewritten[0].Text != "other continued outside Mindwire" || parses.Load() != 3 {
		t.Fatal("native replacement was hidden by cache")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Read(path, "chat", parse); !os.IsNotExist(err) {
		t.Fatal("deleted transcript served from cache")
	}
}

func TestNativeHistoryCacheBoundsMemoryAndDoesNotCacheUnfinishedReads(t *testing.T) {
	cache := newHistoryCache(1, 1024)
	path := filepath.Join(t.TempDir(), "native.jsonl")
	if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	parse := func(f *os.File) ([]Message, error) { return []Message{{ID: "native", Text: "saved"}}, nil }
	if _, err := cache.Read(path, "one", parse); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Read(path, "two", parse); err != nil {
		t.Fatal(err)
	}
	if len(cache.entries) != 1 || cache.entries[historyCacheKey{path, "two"}] == nil {
		t.Fatal("LRU bound not enforced")
	}
	_, err := cache.Read(path, "changing", func(f *os.File) ([]Message, error) {
		return []Message{{Text: "prefix"}}, os.WriteFile(path, []byte("native data changed during parsing"), 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	if cache.entries[historyCacheKey{path, "changing"}] != nil {
		t.Fatal("cached a changing transcript")
	}
}

func TestNativeHistoryCacheDoesNotShareMutableProtocolFields(t *testing.T) {
	cache := newHistoryCache(1, 1<<20)
	path := filepath.Join(t.TempDir(), "native.jsonl")
	if err := os.WriteFile(path, []byte("native"), 0600); err != nil {
		t.Fatal(err)
	}
	parse := func(*os.File) ([]Message, error) {
		return []Message{{ID: "saved", Parts: []Part{
			{Type: "tool", Tool: &ToolPart{Input: []byte(`{"key":"value"}`), Action: &ToolAction{
				Kind: KindFileEdit, Files: []FileChange{{Path: "file.swift", Diff: "+saved"}}}}},
			{Type: "interaction", Interaction: &Interaction{Meta: map[string]any{"nested": map[string]any{"saved": true}}}},
		}}}, nil
	}
	first, err := cache.Read(path, "chat", parse)
	if err != nil {
		t.Fatal(err)
	}
	first[0].ID = "changed"
	first[0].Parts[0].Tool.Input[0] = '['
	first[0].Parts[0].Tool.Action.Files[0].Diff = "changed"
	first[0].Parts[1].Interaction.Meta["nested"].(map[string]any)["saved"] = false
	second, err := cache.Read(path, "chat", parse)
	if err != nil {
		t.Fatal(err)
	}
	if second[0].ID != "saved" || second[0].Parts[0].Tool.Input[0] != '{' ||
		second[0].Parts[0].Tool.Action.Files[0].Diff != "+saved" ||
		second[0].Parts[1].Interaction.Meta["nested"].(map[string]any)["saved"] != true {
		t.Fatal("caller mutation contaminated native transcript cache")
	}
}
