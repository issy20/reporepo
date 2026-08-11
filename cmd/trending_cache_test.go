package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/issy20/reporepo/internal/clients"
)

func testRepos() []clients.TrendingRepo {
	return []clients.TrendingRepo{
		{FullName: "owner/repo", Description: "desc", Stars: 123, Language: "Go"},
	}
}

func TestTrendingCacheFreshReturnsEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trending-cache.json")
	cache := loadTrendingCache(path)
	now := time.Now()
	cache.set("week|50|", testRepos(), now)
	saveTrendingCache(path, cache)

	loaded := loadTrendingCache(path)
	repos, ok := loaded.fresh("week|50|", now.Add(time.Hour), DefaultTrendingCacheTTL)
	if !ok || len(repos) != 1 || repos[0].FullName != "owner/repo" {
		t.Fatalf("fresh() = %#v, %v", repos, ok)
	}
}

func TestTrendingCacheExpiredIsMiss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trending-cache.json")
	cache := loadTrendingCache(path)
	now := time.Now()
	cache.set("week|50|", testRepos(), now)
	saveTrendingCache(path, cache)

	loaded := loadTrendingCache(path)
	if _, ok := loaded.fresh("week|50|", now.Add(DefaultTrendingCacheTTL+time.Minute), DefaultTrendingCacheTTL); ok {
		t.Fatal("fresh() returned stale entry")
	}
}

func TestTrendingCacheDistinctQueryIsSeparateEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trending-cache.json")
	cache := loadTrendingCache(path)
	now := time.Now()
	cache.set("week|50|go", []clients.TrendingRepo{{FullName: "a/b"}}, now)
	cache.set("month|100|", []clients.TrendingRepo{{FullName: "c/d"}}, now)
	saveTrendingCache(path, cache)

	loaded := loadTrendingCache(path)
	repos, ok := loaded.fresh("week|50|go", now.Add(time.Hour), DefaultTrendingCacheTTL)
	if !ok || len(repos) != 1 || repos[0].FullName != "a/b" {
		t.Fatalf("week entry = %#v, %v", repos, ok)
	}
	repos, ok = loaded.fresh("month|100|", now.Add(time.Hour), DefaultTrendingCacheTTL)
	if !ok || len(repos) != 1 || repos[0].FullName != "c/d" {
		t.Fatalf("month entry = %#v, %v", repos, ok)
	}
}

func TestTrendingCacheBrokenFileIsMiss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trending-cache.json")
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}

	cache := loadTrendingCache(path)
	if _, ok := cache.fresh("week|50|", time.Now(), DefaultTrendingCacheTTL); ok {
		t.Fatal("broken cache was treated as a hit")
	}
}

func TestTrendingCacheMissingFileIsMiss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trending-cache.json")
	cache := loadTrendingCache(path)
	if _, ok := cache.fresh("week|50|", time.Now(), DefaultTrendingCacheTTL); ok {
		t.Fatal("missing cache file was treated as a hit")
	}
}

func TestTrendingCacheAnyReturnsStaleEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trending-cache.json")
	cache := loadTrendingCache(path)
	now := time.Now()
	cache.set("week|50|", testRepos(), now.Add(-DefaultTrendingCacheTTL-time.Hour))
	saveTrendingCache(path, cache)

	loaded := loadTrendingCache(path)
	repos, ok := loaded.any("week|50|")
	if !ok || len(repos) != 1 {
		t.Fatalf("any() = %#v, %v", repos, ok)
	}
}

func TestTrendingCachePathUsesDataDirectory(t *testing.T) {
	got := trendingCachePath("/tmp/x/data.json")
	if !strings.HasSuffix(got, "/x/trending-cache.json") {
		t.Fatalf("trendingCachePath() = %q", got)
	}
}
