package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/issy20/reporepo/internal/clients"
)

// DefaultTrendingCacheTTL は疑似Trending一覧のキャッシュ有効期間。
const DefaultTrendingCacheTTL = 6 * time.Hour

const trendingCacheVersion = 1

type trendingCacheEntry struct {
	FetchedAt time.Time              `json:"fetched_at"`
	Repos     []clients.TrendingRepo `json:"repos"`
}

type trendingCache struct {
	Version int                           `json:"version"`
	Entries map[string]trendingCacheEntry `json:"entries"`
}

// trendingCacheKey はクエリ別エントリの正規化キー。
func trendingCacheKey(since string, minStars int, language string) string {
	return since + "|" + strconv.Itoa(minStars) + "|" + language
}

// trendingCachePath は data.json のディレクトリ配下のキャッシュファイルパスを返す。
func trendingCachePath(dataPath string) string {
	return filepath.Join(filepath.Dir(dataPath), "trending-cache.json")
}

// loadTrendingCache はキャッシュを読み込む。ファイルがない・壊れている場合は空キャッシュとして扱う。
func loadTrendingCache(path string) *trendingCache {
	cache := &trendingCache{Version: trendingCacheVersion, Entries: map[string]trendingCacheEntry{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return cache
	}
	if err := json.Unmarshal(data, cache); err != nil {
		return &trendingCache{Version: trendingCacheVersion, Entries: map[string]trendingCacheEntry{}}
	}
	if cache.Entries == nil {
		cache.Entries = map[string]trendingCacheEntry{}
	}
	return cache
}

// fresh はTTL以内の新鮮なエントリを返す。
func (c *trendingCache) fresh(key string, now time.Time, ttl time.Duration) ([]clients.TrendingRepo, bool) {
	entry, ok := c.Entries[key]
	if !ok || now.Sub(entry.FetchedAt) > ttl {
		return nil, false
	}
	return entry.Repos, true
}

// any は鮮度に関わらずエントリを返す（レート制限時のフォールバック用）。
func (c *trendingCache) any(key string) ([]clients.TrendingRepo, bool) {
	entry, ok := c.Entries[key]
	if !ok {
		return nil, false
	}
	return entry.Repos, true
}

func (c *trendingCache) set(key string, repos []clients.TrendingRepo, now time.Time) {
	c.Entries[key] = trendingCacheEntry{FetchedAt: now, Repos: repos}
}

// saveTrendingCache は一時ファイル経由のrenameでアトミックに保存する。
func saveTrendingCache(path string, cache *trendingCache) error {
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
