package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/issy20/reporepo/internal/clients"
	"github.com/issy20/reporepo/internal/core"
	"github.com/issy20/reporepo/internal/trendingcache"
)

func testTrendingRepos() []clients.TrendingRepo {
	return []clients.TrendingRepo{
		{FullName: "owner/repo", Description: "A repo", Stars: 123, Language: "Go"},
		{FullName: "other/repo", Description: "Another", Stars: 456, Language: "Rust"},
	}
}

func trendingModel(gh clients.GitHubClient) Model {
	return NewModel(Dependencies{
		Store:  &fakeStore{},
		GitHub: gh,
		AI:     map[string]clients.AIClient{"claude": &fakeAI{}},
		Now:    func() time.Time { return time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC) },
	}, nil)
}

func TestTrendingKeyStartsLoad(t *testing.T) {
	m := trendingModel(&fakeGitHub{trending: testTrendingRepos()})
	got, cmd := updated(t, m, runeKey('t'))
	if got.state != stateTrending || cmd == nil || !got.trendingLoading || got.trendingRequestID != 1 {
		t.Fatalf("state=%v cmd=%v loading=%v id=%d", got.state, cmd, got.trendingLoading, got.trendingRequestID)
	}
}

func TestTrendingKeyWhileTypingGoesToTextInput(t *testing.T) {
	m := trendingModel(&fakeGitHub{})
	m.input.SetValue("owner/")
	got, _ := updated(t, m, runeKey('t'))
	if got.state != stateInput || got.input.Value() != "owner/t" {
		t.Fatalf("state=%v input=%q", got.state, got.input.Value())
	}
}

func TestTrendingKeyWithoutGitHubIsNoop(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	got, cmd := updated(t, m, runeKey('t'))
	if cmd != nil || got.state != stateInput || got.errMessage == "" {
		t.Fatalf("cmd=%v state=%v err=%q", cmd, got.state, got.errMessage)
	}
}

func TestTrendingLoadedSetsRepos(t *testing.T) {
	m := trendingModel(&fakeGitHub{trending: testTrendingRepos()})
	loading, _ := updated(t, m, runeKey('t'))
	got, _ := updated(t, loading, trendingLoadedMsg{requestID: loading.trendingRequestID, repos: testTrendingRepos()})
	if got.state != stateTrending || got.trendingLoading || len(got.trendingRepos) != 2 {
		t.Fatalf("state=%v loading=%v repos=%#v", got.state, got.trendingLoading, got.trendingRepos)
	}
}

func TestTrendingFailedSetsError(t *testing.T) {
	m := trendingModel(&fakeGitHub{})
	loading, _ := updated(t, m, runeKey('t'))
	got, _ := updated(t, loading, trendingFailedMsg{requestID: loading.trendingRequestID, err: errTrendingFetch})
	if got.trendingLoading || got.trendingErr != errTrendingFetch.Error() {
		t.Fatalf("loading=%v err=%q", got.trendingLoading, got.trendingErr)
	}
}

func TestTrendingOldResultIsIgnored(t *testing.T) {
	m := trendingModel(&fakeGitHub{})
	loading, _ := updated(t, m, runeKey('t'))
	id := loading.trendingRequestID
	got, _ := updated(t, loading, trendingLoadedMsg{requestID: id - 1, repos: testTrendingRepos()})
	if len(got.trendingRepos) != 0 {
		t.Fatalf("old trending result applied: %#v", got.trendingRepos)
	}
}

func TestTrendingNavigation(t *testing.T) {
	m := trendingModel(&fakeGitHub{trending: testTrendingRepos()})
	m.trendingRepos = testTrendingRepos()
	m.state = stateTrending
	down, _ := updated(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if down.trendingSelected != 1 {
		t.Fatalf("selected after down = %d", down.trendingSelected)
	}
	up, _ := updated(t, down, tea.KeyMsg{Type: tea.KeyUp})
	if up.trendingSelected != 0 {
		t.Fatalf("selected after up = %d", up.trendingSelected)
	}
}

func TestTrendingEnterStartsAnalysis(t *testing.T) {
	s, gh := &recordingStore{}, &fakeGitHub{trending: testTrendingRepos()}
	m := commandModel(s, gh, &fakeAI{analysis: &core.Analysis{}})
	m.state = stateTrending
	m.trendingRepos = testTrendingRepos()
	got, cmd := updated(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if got.state != stateLoading || cmd == nil || got.loadingLabel != "解析しています: owner/repo" {
		t.Fatalf("state=%v cmd=%v label=%q", got.state, cmd, got.loadingLabel)
	}
}

func TestTrendingEnterOnEmptyListIsNoop(t *testing.T) {
	m := trendingModel(&fakeGitHub{})
	m.state = stateTrending
	got, cmd := updated(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || got.state != stateTrending {
		t.Fatalf("cmd=%v state=%v", cmd, got.state)
	}
}

func TestTrendingEscapeReturnsToInputAndInvalidatesRequest(t *testing.T) {
	m := trendingModel(&fakeGitHub{})
	m.state = stateTrending
	m.trendingRequestID = 5
	got, cmd := updated(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil || got.state != stateInput || got.trendingRequestID != 6 {
		t.Fatalf("cmd=%v state=%v id=%d", cmd, got.state, got.trendingRequestID)
	}
}

func TestTrendingStaleLoadedShowsNotice(t *testing.T) {
	m := trendingModel(&fakeGitHub{})
	m.trendingRequestID = 1
	got, _ := updated(t, m, trendingLoadedMsg{requestID: 1, repos: testTrendingRepos(), stale: true})
	if !got.trendingStale || got.trendingErr == "" {
		t.Fatalf("stale=%v err=%q", got.trendingStale, got.trendingErr)
	}
}

func TestTrendingViewRendersListAndSanitizes(t *testing.T) {
	gh := &fakeGitHub{trending: []clients.TrendingRepo{
		{FullName: "owner/repo", Description: "bad\x1b[31mANSI\x1b[0m\x00ctrl", Stars: 123, Language: "Go"},
	}}
	m := trendingModel(gh)
	m.state = stateTrending
	m.trendingRepos = gh.trending
	view := m.View()
	if !strings.Contains(view, "owner/repo") || !strings.Contains(view, "123") {
		t.Fatalf("view missing list: %q", view)
	}
	if strings.Contains(view, "\x1b[") || strings.Contains(view, "\x00") {
		t.Fatalf("view contains control characters: %q", view)
	}
	if !strings.Contains(view, "ANSI") || !strings.Contains(view, "ctrl") {
		t.Fatalf("view lost description content: %q", view)
	}
}

func TestTrendingViewLoading(t *testing.T) {
	m := trendingModel(&fakeGitHub{})
	m.state = stateTrending
	m.trendingLoading = true
	view := m.View()
	if !strings.Contains(view, "取得しています") {
		t.Fatalf("loading view = %q", view)
	}
}

func TestTrendingViewEmpty(t *testing.T) {
	m := trendingModel(&fakeGitHub{})
	m.state = stateTrending
	view := m.View()
	if !strings.Contains(view, "該当するリポジトリはありません") {
		t.Fatalf("empty view = %q", view)
	}
}

func TestTrendingViewShowsError(t *testing.T) {
	m := trendingModel(&fakeGitHub{})
	m.state = stateTrending
	m.trendingErr = errTrendingFetch.Error()
	view := m.View()
	if !strings.Contains(view, "取得できませんでした") {
		t.Fatalf("error view = %q", view)
	}
}

func TestTrendingCmdCacheHitDoesNotFetch(t *testing.T) {
	gh := &fakeGitHub{trending: testTrendingRepos()}
	path := filepath.Join(t.TempDir(), "trending-cache.json")
	cache := trendingcache.Load(path)
	cache.Set(trendingcache.Key("week", 50, ""), testTrendingRepos(), time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))
	trendingcache.Save(path, cache)

	m := NewModel(Dependencies{
		Store:             &fakeStore{},
		GitHub:            gh,
		AI:                map[string]clients.AIClient{"claude": &fakeAI{}},
		Now:               func() time.Time { return time.Date(2026, 8, 10, 13, 0, 0, 0, time.UTC) },
		TrendingCachePath: path,
	}, nil)
	msg := m.trendingCmd(1)()
	loaded, ok := msg.(trendingLoadedMsg)
	if !ok || loaded.stale || len(loaded.repos) != 2 {
		t.Fatalf("msg = %#v", msg)
	}
	if gh.trendingCalls != 0 {
		t.Fatalf("SearchTrending calls = %d, want 0 on cache hit", gh.trendingCalls)
	}
}

func TestTrendingCmdFetchesAndCaches(t *testing.T) {
	gh := &fakeGitHub{trending: testTrendingRepos()}
	path := filepath.Join(t.TempDir(), "trending-cache.json")
	m := NewModel(Dependencies{
		Store:             &fakeStore{},
		GitHub:            gh,
		AI:                map[string]clients.AIClient{"claude": &fakeAI{}},
		Now:               func() time.Time { return time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC) },
		TrendingCachePath: path,
	}, nil)
	msg := m.trendingCmd(1)()
	if _, ok := msg.(trendingLoadedMsg); !ok {
		t.Fatalf("msg = %#v", msg)
	}
	if gh.trendingCalls != 1 {
		t.Fatalf("SearchTrending calls = %d, want 1", gh.trendingCalls)
	}
	cache := trendingcache.Load(path)
	if _, ok := cache.Fresh(trendingcache.Key("week", 50, ""), time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC), trendingcache.DefaultTTL); !ok {
		t.Fatal("fetched repos were not cached")
	}
}

func TestTrendingCmdRateLimitUsesStaleCache(t *testing.T) {
	gh := &fakeGitHub{trendingErr: clients.ErrTrendingRateLimited}
	path := filepath.Join(t.TempDir(), "trending-cache.json")
	cache := trendingcache.Load(path)
	cache.Set(trendingcache.Key("week", 50, ""), testTrendingRepos(), time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	trendingcache.Save(path, cache)

	m := NewModel(Dependencies{
		Store:             &fakeStore{},
		GitHub:            gh,
		AI:                map[string]clients.AIClient{"claude": &fakeAI{}},
		Now:               func() time.Time { return time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC) },
		TrendingCachePath: path,
	}, nil)
	msg := m.trendingCmd(1)()
	loaded, ok := msg.(trendingLoadedMsg)
	if !ok || !loaded.stale || len(loaded.repos) != 2 {
		t.Fatalf("msg = %#v, want stale loaded", msg)
	}
}

func TestTrendingCmdRateLimitWithoutCacheFails(t *testing.T) {
	gh := &fakeGitHub{trendingErr: clients.ErrTrendingRateLimited}
	m := NewModel(Dependencies{
		Store:  &fakeStore{},
		GitHub: gh,
		AI:     map[string]clients.AIClient{"claude": &fakeAI{}},
		Now:    func() time.Time { return time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC) },
	}, nil)
	msg := m.trendingCmd(1)()
	failed, ok := msg.(trendingFailedMsg)
	if !ok || !strings.Contains(failed.err.Error(), "レート制限") {
		t.Fatalf("msg = %#v, want rate limit failure", msg)
	}
}
