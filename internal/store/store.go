package store

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/yourname/reporepo/internal/core"
)

// Store はJSONファイルへの永続化を担当する。
type Store struct {
	filepath string
}

// replaceFile is a test seam for simulating a failure at the atomic replace step.
var replaceFile = os.Rename

// NewStore は新しい Store を初期化する。
func NewStore(filepath string) *Store {
	return &Store{filepath: filepath}
}

// Load はJSONファイルから全エントリを読み込む。
func (s *Store) Load() ([]*core.Entry, error) {
	data, err := os.ReadFile(s.filepath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []*core.Entry{}, nil
		}
		return nil, err
	}

	var entries []*core.Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}

	cleaned := make([]*core.Entry, 0, len(entries))
	for _, e := range entries {
		if e != nil {
			cleaned = append(cleaned, e)
		}
	}
	return cleaned, nil
}

// Save は全エントリを一時ファイル経由でアトミックに保存する。
func (s *Store) Save(entries []*core.Entry) error {
	dir := filepath.Dir(s.filepath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(dir, "data.*.json")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		_ = os.Remove(tmpPath)
	}()

	if err := tmpFile.Chmod(0600); err != nil {
		return err
	}

	if _, err := tmpFile.Write(data); err != nil {
		return err
	}

	if err := tmpFile.Sync(); err != nil {
		return err
	}

	if err := tmpFile.Close(); err != nil {
		return err
	}

	if err := replaceFile(tmpPath, s.filepath); err != nil {
		return err
	}

	return nil
}

func (s *Store) Upsert(entry *core.Entry) error {
	if entry == nil {
		return errors.New("cannot upsert nil entry")
	}

	entries, err := s.Load()
	if err != nil {
		return err
	}

	var found *core.Entry
	for _, e := range entries {
		if e.FullName == entry.FullName {
			found = e
			break
		}
	}

	if found != nil {
		found.RepoMeta = entry.RepoMeta
		found.IsFavorite = entry.IsFavorite
		found.ViewedAt = entry.ViewedAt

		if found.Analyses == nil {
			found.Analyses = make(map[string]*core.Analysis)
		}
		for lang, analysis := range entry.Analyses {
			found.Analyses[lang] = analysis
		}
	} else {
		if entry.CreatedAt.IsZero() {
			entry.CreatedAt = entry.ViewedAt
		}
		entries = append(entries, entry)
	}

	return s.Save(entries)
}
