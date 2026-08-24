package store

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/issy20/reporepo/internal/core"
)

// lockTimeout はロック取得の待機上限。超過時は「保存中」エラーを返す。
var lockTimeout = 5 * time.Second

// Store はJSONファイルへの永続化を担当する。
type Store struct {
	filepath string
	lockPath string
}

// replaceFile is a test seam for simulating a failure at the atomic replace step.
var replaceFile = os.Rename

// NewStore は新しい Store を初期化する。
func NewStore(filepath string) *Store {
	return &Store{filepath: filepath, lockPath: filepath + ".lock"}
}

// withLock は read-modify-write 全体をロックの critical section で実行する。
func (s *Store) withLock(fn func() error) error {
	lock := flock.New(s.lockPath)
	ctx, cancel := context.WithTimeout(context.Background(), lockTimeout)
	defer cancel()
	ok, err := lock.TryLockContext(ctx, 50*time.Millisecond)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return errors.New("他のプロセスが保存中です。再実行してください")
		}
		return err
	}
	if !ok {
		return errors.New("他のプロセスが保存中です。再実行してください")
	}
	defer lock.Unlock()
	return fn()
}

// Load はJSONファイルから全エントリを読み込む。
func (s *Store) Load() ([]*core.Entry, error) {
	var result []*core.Entry
	err := s.withLock(func() error {
		entries, err := s.load()
		if err != nil {
			return err
		}
		result = entries
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// load はロックなしで全エントリを読み込む。
func (s *Store) load() ([]*core.Entry, error) {
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

// save は全エントリを一時ファイル経由でアトミックに保存する（ロックなし内部実装）。
func (s *Store) save(entries []*core.Entry) error {
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

// Save は全エントリをロック内でアトミックに保存する。
func (s *Store) Save(entries []*core.Entry) error {
	return s.withLock(func() error {
		return s.save(entries)
	})
}

func (s *Store) Upsert(entry *core.Entry) error {
	if entry == nil {
		return errors.New("cannot upsert nil entry")
	}

	return s.withLock(func() error {
		entries, err := s.load()
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

		return s.save(entries)
	})
}

// Delete は fullName に一致するエントリをロック内で削除する（大文字小文字を無視）。
func (s *Store) Delete(fullName string) error {
	return s.withLock(func() error {
		entries, err := s.load()
		if err != nil {
			return err
		}
		filtered := make([]*core.Entry, 0, len(entries))
		for _, e := range entries {
			if e != nil && !strings.EqualFold(e.FullName, fullName) {
				filtered = append(filtered, e)
			}
		}
		return s.save(filtered)
	})
}
