package trendingcache

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

func TestFreshReturnsEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trending-cache.json")
	cache := Load(path)
	now := time.Now()
	cache.Set("week|50|", testRepos(), now)
	Save(path, cache)

	loaded := Load(path)
	repos, ok := loaded.Fresh("week|50|", now.Add(time.Hour), DefaultTTL)
	if !ok || len(repos) != 1 || repos[0].FullName != "owner/repo" {
		t.Fatalf("Fresh() = %#v, %v", repos, ok)
	}
}

func TestExpiredIsMiss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trending-cache.json")
	cache := Load(path)
	now := time.Now()
	cache.Set("week|50|", testRepos(), now)
	Save(path, cache)

	loaded := Load(path)
	if _, ok := loaded.Fresh("week|50|", now.Add(DefaultTTL+time.Minute), DefaultTTL); ok {
		t.Fatal("Fresh() returned stale entry")
	}
}

func TestDistinctQueryIsSeparateEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trending-cache.json")
	cache := Load(path)
	now := time.Now()
	cache.Set("week|50|go", []clients.TrendingRepo{{FullName: "a/b"}}, now)
	cache.Set("month|100|", []clients.TrendingRepo{{FullName: "c/d"}}, now)
	Save(path, cache)

	loaded := Load(path)
	repos, ok := loaded.Fresh("week|50|go", now.Add(time.Hour), DefaultTTL)
	if !ok || len(repos) != 1 || repos[0].FullName != "a/b" {
		t.Fatalf("week entry = %#v, %v", repos, ok)
	}
	repos, ok = loaded.Fresh("month|100|", now.Add(time.Hour), DefaultTTL)
	if !ok || len(repos) != 1 || repos[0].FullName != "c/d" {
		t.Fatalf("month entry = %#v, %v", repos, ok)
	}
}

func TestBrokenFileIsMiss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trending-cache.json")
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}

	cache := Load(path)
	if _, ok := cache.Fresh("week|50|", time.Now(), DefaultTTL); ok {
		t.Fatal("broken cache was treated as a hit")
	}
}

func TestMissingFileIsMiss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trending-cache.json")
	cache := Load(path)
	if _, ok := cache.Fresh("week|50|", time.Now(), DefaultTTL); ok {
		t.Fatal("missing cache file was treated as a hit")
	}
}

func TestAnyReturnsStaleEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trending-cache.json")
	cache := Load(path)
	now := time.Now()
	cache.Set("week|50|", testRepos(), now.Add(-DefaultTTL-time.Hour))
	Save(path, cache)

	loaded := Load(path)
	repos, ok := loaded.Any("week|50|")
	if !ok || len(repos) != 1 {
		t.Fatalf("Any() = %#v, %v", repos, ok)
	}
}

func TestPathUsesDataDirectory(t *testing.T) {
	got := Path("/tmp/x/data.json")
	if !strings.HasSuffix(got, "/x/trending-cache.json") {
		t.Fatalf("Path() = %q", got)
	}
}

func TestKeyNormalizesQuery(t *testing.T) {
	if got := Key("week", 50, "go"); got != "week|50|go" {
		t.Fatalf("Key() = %q", got)
	}
	if got := Key("month", 100, ""); got != "month|100|" {
		t.Fatalf("Key() = %q", got)
	}
}
