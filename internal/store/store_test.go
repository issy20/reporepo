package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yourname/reporepo/internal/core"
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
		if file.Name() != "data.json" {
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
