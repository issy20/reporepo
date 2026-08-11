package trendingcache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/issy20/reporepo/internal/clients"
)

// DefaultTTL は疑似Trending一覧のキャッシュ有効期間。
const DefaultTTL = 6 * time.Hour

const version = 1

type entry struct {
	FetchedAt time.Time              `json:"fetched_at"`
	Repos     []clients.TrendingRepo `json:"repos"`
}

// Cache は疑似Trending一覧のファイルキャッシュ。
type Cache struct {
	Version int              `json:"version"`
	Entries map[string]entry `json:"entries"`
}

// Key はクエリ別エントリの正規化キー。
func Key(since string, minStars int, language string) string {
	return since + "|" + strconv.Itoa(minStars) + "|" + language
}

// Path は data.json のディレクトリ配下のキャッシュファイルパスを返す。
func Path(dataPath string) string {
	return filepath.Join(filepath.Dir(dataPath), "trending-cache.json")
}

// Load はキャッシュを読み込む。ファイルがない・壊れている場合は空キャッシュとして扱う。
func Load(path string) *Cache {
	cache := &Cache{Version: version, Entries: map[string]entry{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return cache
	}
	if err := json.Unmarshal(data, cache); err != nil {
		return &Cache{Version: version, Entries: map[string]entry{}}
	}
	if cache.Entries == nil {
		cache.Entries = map[string]entry{}
	}
	return cache
}

// Fresh はTTL以内の新鮮なエントリを返す。
func (c *Cache) Fresh(key string, now time.Time, ttl time.Duration) ([]clients.TrendingRepo, bool) {
	entry, ok := c.Entries[key]
	if !ok || now.Sub(entry.FetchedAt) > ttl {
		return nil, false
	}
	return entry.Repos, true
}

// Any は鮮度に関わらずエントリを返す（レート制限時のフォールバック用）。
func (c *Cache) Any(key string) ([]clients.TrendingRepo, bool) {
	entry, ok := c.Entries[key]
	if !ok {
		return nil, false
	}
	return entry.Repos, true
}

// Set はエントリをメモリ上に記録する。
func (c *Cache) Set(key string, repos []clients.TrendingRepo, now time.Time) {
	c.Entries[key] = entry{FetchedAt: now, Repos: repos}
}

// Save は一時ファイル経由のrenameでアトミックに保存する。
func Save(path string, cache *Cache) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	tmpFile, err := os.CreateTemp(dir, "trending-cache.*.json")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		_ = os.Remove(tmpPath)
	}()

	if err := tmpFile.Chmod(0o600); err != nil {
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
	return os.Rename(tmpPath, path)
}
