package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/issy20/reporepo/internal/analyzer"
	"github.com/issy20/reporepo/internal/clients"
	"github.com/issy20/reporepo/internal/core"
	"github.com/issy20/reporepo/internal/trendingcache"
)

type analysisSucceededMsg struct {
	requestID uint64
	entry     *core.Entry
	warnings  []string
}
type analysisFailedMsg struct {
	requestID uint64
	err       error
}

type trendingLoadedMsg struct {
	requestID uint64
	repos     []clients.TrendingRepo
	stale     bool
}
type trendingFailedMsg struct {
	requestID uint64
	err       error
}

var errTrendingFetch = errors.New("Trending 一覧を取得できませんでした")

type entryMutationKind uint8

const (
	mutationFavorite entryMutationKind = iota
	mutationDelete
	mutationNote
)

type entryMutationFinishedMsg struct {
	requestID uint64
	kind      entryMutationKind
	fullName  string
	err       error
}

func (m Model) analyzeCmd(ctx context.Context, input string, force bool, requestID uint64) tea.Cmd {
	return func() tea.Msg {
		result, err := m.analyze(ctx, input, force)
		if err != nil {
			return analysisFailedMsg{requestID: requestID, err: err}
		}
		return analysisSucceededMsg{requestID: requestID, entry: result.Entry, warnings: result.Warnings}
	}
}

func (m Model) analyze(ctx context.Context, input string, force bool) (*analyzer.Result, error) {
	return m.analyzer.Analyze(ctx, input, m.language, m.provider, force)
}

// saveNoteCmd はノートを付けたエントリの複製を非同期で Upsert する。
func (m Model) saveNoteCmd(note string) tea.Cmd {
	updated := analyzer.CloneEntry(m.current)
	updated.Note = note
	requestID := m.mutationRequestID
	fullName := m.current.FullName
	store := m.store
	return func() tea.Msg {
		err := store.Upsert(updated)
		return entryMutationFinishedMsg{requestID: requestID, kind: mutationNote, fullName: fullName, err: userStoreError(err)}
	}
}

// trendingCmd は疑似Trending一覧をキャッシュ確認 → 取得 → 保存の順で非同期実行する。
func (m Model) trendingCmd(requestID uint64) tea.Cmd {
	return func() tea.Msg {
		now := m.now()
		key := trendingcache.Key("week", 50, "")
		if m.trendingCachePath != "" {
			cache := trendingcache.Load(m.trendingCachePath)
			if repos, ok := cache.Fresh(key, now, trendingcache.DefaultTTL); ok {
				return trendingLoadedMsg{requestID: requestID, repos: repos}
			}
		}
		repos, err := m.github.SearchTrending(context.Background(), clients.TrendingQuery{CreatedAfter: now.AddDate(0, 0, -7), MinStars: 50})
		if errors.Is(err, clients.ErrTrendingRateLimited) {
			if m.trendingCachePath != "" {
				if repos, ok := trendingcache.Load(m.trendingCachePath).Any(key); ok {
					return trendingLoadedMsg{requestID: requestID, repos: repos, stale: true}
				}
			}
			return trendingFailedMsg{requestID: requestID, err: errors.New("GitHub Search API のレート制限に達しました。時間をおいて再実行してください")}
		}
		if err != nil {
			return trendingFailedMsg{requestID: requestID, err: errTrendingFetch}
		}
		if m.trendingCachePath != "" {
			cache := trendingcache.Load(m.trendingCachePath)
			cache.Set(key, repos, now)
			_ = trendingcache.Save(m.trendingCachePath, cache)
		}
		return trendingLoadedMsg{requestID: requestID, repos: repos}
	}
}
