package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"github.com/issy20/reporepo/internal/core"
)

func TestStore_SaveAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "data.json")
	s := NewStore(dbPath)

	entries, err := s.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty entries, got %d", len(entries))
	}

	now := time.Now().Round(time.Second)
	entry := &core.Entry{
		FullName: "test-owner/test-repo",
		RepoMeta: &core.RepoMeta{
			FullName:    "test-owner/test-repo",
			Description: "description",
			Stars:       10,
		},
		Analyses:   make(map[string]*core.Analysis),
		IsFavorite: true,
		ViewedAt:   now,
		CreatedAt:  now,
	}

	err = s.Save([]*core.Entry{entry})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(loaded))
	}

	if loaded[0].FullName != entry.FullName {
		t.Errorf("expected FullName %s, got %s", entry.FullName, loaded[0].FullName)
	}
	if loaded[0].RepoMeta.Stars != entry.RepoMeta.Stars {
		t.Errorf("expected Stars %d, got %d", entry.RepoMeta.Stars, loaded[0].RepoMeta.Stars)
	}
	if !loaded[0].ViewedAt.Equal(entry.ViewedAt) {
		t.Errorf("expected ViewedAt %v, got %v", entry.ViewedAt, loaded[0].ViewedAt)
	}
}

func TestStore_Upsert(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "data.json")
	s := NewStore(dbPath)

	now := time.Now().Round(time.Second)
	entry := &core.Entry{
		FullName: "test-owner/test-repo",
		RepoMeta: &core.RepoMeta{
			FullName:    "test-owner/test-repo",
			Description: "first desc",
			Stars:       10,
		},
		Analyses: map[string]*core.Analysis{
			"ja": {
				Summary: "日本語の要約",
			},
		},
		IsFavorite: false,
		ViewedAt:   now.Add(-10 * time.Minute),
		CreatedAt:  now.Add(-10 * time.Minute),
	}

	if err := s.Upsert(entry); err != nil {
		t.Fatalf("first Upsert failed: %v", err)
	}

	newViewedAt := now
	entry2 := &core.Entry{
		FullName: "test-owner/test-repo",
		RepoMeta: &core.RepoMeta{
			FullName:    "test-owner/test-repo",
			Description: "second desc",
			Stars:       20,
		},
		Analyses: map[string]*core.Analysis{
			"en": {
				Summary: "English Summary",
			},
		},
		IsFavorite: true,
		ViewedAt:   newViewedAt,
	}

	if err := s.Upsert(entry2); err != nil {
		t.Fatalf("second Upsert failed: %v", err)
	}

	loaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(loaded) != 1 {
		t.Fatalf("expected exactly 1 entry after upsert, got %d", len(loaded))
	}

	result := loaded[0]
	if result.RepoMeta.Stars != 20 {
		t.Errorf("expected updated stars 20, got %d", result.RepoMeta.Stars)
	}
	if result.RepoMeta.Description != "second desc" {
		t.Errorf("expected updated description 'second desc', got '%s'", result.RepoMeta.Description)
	}
	if !result.IsFavorite {
		t.Errorf("expected IsFavorite to be true")
	}
	if !result.ViewedAt.Equal(newViewedAt) {
		t.Errorf("expected ViewedAt updated to %v, got %v", newViewedAt, result.ViewedAt)
	}
	if len(result.Analyses) != 2 {
		t.Errorf("expected 2 analyses (ja, en), got %d", len(result.Analyses))
	}
	if result.Analyses["ja"].Summary != "日本語の要約" {
		t.Errorf("expected ja analysis to persist")
	}
	if result.Analyses["en"].Summary != "English Summary" {
		t.Errorf("expected en analysis to be added")
	}
}

func TestStore_SaveFail_Rename(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "data.json")
	s := NewStore(dbPath)

	oldEntry := &core.Entry{FullName: "old/repo"}
	if err := s.Save([]*core.Entry{oldEntry}); err != nil {
		t.Fatalf("initial save failed: %v", err)
	}

	originalReplaceFile := replaceFile
	replaceFile = func(_, _ string) error { return errors.New("forced rename failure") }
	t.Cleanup(func() { replaceFile = originalReplaceFile })

	newEntry := &core.Entry{FullName: "new/repo"}
	if err := s.Save([]*core.Entry{newEntry}); err == nil {
		t.Fatal("expected save to fail at rename")
	}

	loaded, err := s.Load()
	if err != nil {
		t.Fatalf("load after failed save: %v", err)
	}
	if len(loaded) != 1 || loaded[0].FullName != oldEntry.FullName {
		t.Fatalf("existing data was replaced after failed save: %#v", loaded)
	}

	files, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("read temp directory: %v", err)
	}
	for _, file := range files {
		if file.Name() != "data.json" && file.Name() != "data.json.lock" {
			t.Errorf("temporary file leaked after failed save: %s", file.Name())
		}
	}
}

func TestStore_UpsertNil(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "data.json")
	s := NewStore(dbPath)

	err := s.Upsert(nil)
	if err == nil {
		t.Errorf("expected Upsert(nil) to return error, got nil")
	}
}

func TestStore_LoadNull(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "data.json")
	s := NewStore(dbPath)

	if err := os.WriteFile(dbPath, []byte("null"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	entries, err := s.Load()
	if err != nil {
		t.Fatalf("Load with 'null' failed: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty entries for 'null' JSON, got %d", len(entries))
	}

	if err := os.WriteFile(dbPath, []byte("invalid json"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	_, err = s.Load()
	if err == nil {
		t.Errorf("expected error loading invalid json, got nil")
	}
}

func TestStore_RepeatedSave(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "data.json")
	s := NewStore(dbPath)

	for i := 0; i < 10; i++ {
		entry := &core.Entry{FullName: "test/repo"}
		if err := s.Save([]*core.Entry{entry}); err != nil {
			t.Fatalf("repeated save failed at iteration %d: %v", i, err)
		}
	}
}

func TestStore_ConcurrentUpsertKeepsAllEntries(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "data.json")
	s := NewStore(dbPath)

	const n = 20
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errCh <- s.Upsert(&core.Entry{FullName: fmt.Sprintf("owner/repo-%d", i)})
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent Upsert failed: %v", err)
		}
	}

	loaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded) != n {
		t.Fatalf("expected %d entries, got %d (lost update)", n, len(loaded))
	}
	seen := make(map[string]bool)
	for _, e := range loaded {
		seen[e.FullName] = true
	}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("owner/repo-%d", i)
		if !seen[name] {
			t.Errorf("entry %s was lost", name)
		}
	}
}

func TestStore_ConcurrentUpsertAndDeleteSerialized(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "data.json")
	s := NewStore(dbPath)

	if err := s.Upsert(&core.Entry{FullName: "owner/keep"}); err != nil {
		t.Fatalf("seed upsert failed: %v", err)
	}
	if err := s.Upsert(&core.Entry{FullName: "owner/drop"}); err != nil {
		t.Fatalf("seed upsert failed: %v", err)
	}

	const add = 10
	var wg sync.WaitGroup
	errCh := make(chan error, add+1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		errCh <- s.Delete("owner/drop")
	}()
	for i := 0; i < add; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errCh <- s.Upsert(&core.Entry{FullName: fmt.Sprintf("owner/add-%d", i)})
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent op failed: %v", err)
		}
	}

	loaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	names := make(map[string]bool)
	for _, e := range loaded {
		names[e.FullName] = true
	}
	if names["owner/drop"] {
		t.Error("deleted entry still present")
	}
	if !names["owner/keep"] {
		t.Error("keep entry was lost during concurrent delete")
	}
	for i := 0; i < add; i++ {
		if !names[fmt.Sprintf("owner/add-%d", i)] {
			t.Errorf("added entry owner/add-%d was lost during concurrent delete", i)
		}
	}
}

func TestStore_LockTimeoutReturnsSafeError(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "data.json")
	s := NewStore(dbPath)

	original := lockTimeout
	lockTimeout = 50 * time.Millisecond
	t.Cleanup(func() { lockTimeout = original })

	outer := flock.New(s.lockPath)
	if err := outer.Lock(); err != nil {
		t.Fatalf("outer lock failed: %v", err)
	}
	defer outer.Unlock()

	err := s.Upsert(&core.Entry{FullName: "owner/repo"})
	if err == nil {
		t.Fatal("expected lock timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "保存中") {
		t.Errorf("error = %q, want safe '保存中' message", err.Error())
	}
}

func TestStore_DeleteCaseInsensitive(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "data.json")
	s := NewStore(dbPath)

	if err := s.Upsert(&core.Entry{FullName: "Owner/Repo"}); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	if err := s.Upsert(&core.Entry{FullName: "keep/repo"}); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	if err := s.Delete("owner/repo"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	loaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded) != 1 || loaded[0].FullName != "keep/repo" {
		t.Fatalf("after delete = %#v, want only keep/repo", loaded)
	}
}
