package analyzer

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/issy20/reporepo/internal/clients"
	"github.com/issy20/reporepo/internal/core"
)

type fakeStore struct {
	entries     []*core.Entry
	loadErr     error
	upsertErr   error
	loadCalls   int
	upsertCalls int
	upserted    []*core.Entry
}

func (s *fakeStore) Load() ([]*core.Entry, error) {
	s.loadCalls++
	return s.entries, s.loadErr
}

func (s *fakeStore) Save([]*core.Entry) error { return nil }

func (s *fakeStore) Upsert(e *core.Entry) error {
	s.upsertCalls++
	if s.upsertErr != nil {
		return s.upsertErr
	}
	s.upserted = append(s.upserted, e)
	return nil
}

type fakeGitHub struct {
	calls      int
	data       *clients.RepositoryData
	err        error
	metaCalls  int
	meta       *core.RepoMeta
	metaErr    error
	metaCancel context.CancelFunc
}

func (f *fakeGitHub) FetchRepository(context.Context, string, string) (*clients.RepositoryData, error) {
	f.calls++
	return f.data, f.err
}

func (f *fakeGitHub) FetchRepositoryMeta(ctx context.Context, _, _ string) (*core.RepoMeta, error) {
	f.metaCalls++
	if f.metaCancel != nil {
		f.metaCancel()
	}
	return f.meta, f.metaErr
}

func (f *fakeGitHub) SearchTrending(context.Context, clients.TrendingQuery) ([]clients.TrendingRepo, error) {
	return nil, nil
}

type fakeAI struct {
	calls    int
	analysis *core.Analysis
	err      error
	code     string
}

func (f *fakeAI) Generate(_ context.Context, _ *core.RepoMeta, _, code, _ string) (*core.Analysis, error) {
	f.calls++
	f.code = code
	return f.analysis, f.err
}

func newAnalyzer(s *fakeStore, gh *fakeGitHub, ai *fakeAI) *Analyzer {
	return New(s, gh, map[string]clients.AIClient{"claude": ai}, func() time.Time { return time.Unix(99, 0) }, time.Second)
}

func TestAnalyzeCacheHitDoesNotCallExternalServices(t *testing.T) {
	entry := &core.Entry{FullName: "owner/repo", RepoMeta: &core.RepoMeta{FetchedAt: time.Unix(99, 0)}, Analyses: map[string]*core.Analysis{"ja": {Summary: "cached", PromptVersion: 1, Provider: "claude"}}}
	s, gh, ai := &fakeStore{entries: []*core.Entry{entry}}, &fakeGitHub{}, &fakeAI{}
	got, err := newAnalyzer(s, gh, ai).Analyze(context.Background(), "owner/repo", "ja", "claude", false)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if gh.calls != 0 || ai.calls != 0 || s.upsertCalls != 1 {
		t.Fatalf("calls github=%d ai=%d upsert=%d", gh.calls, ai.calls, s.upsertCalls)
	}
	if got.Entry.ViewedAt != s.upserted[0].ViewedAt || !got.Entry.ViewedAt.Equal(time.Unix(99, 0)) {
		t.Fatalf("ViewedAt not updated: %v", got.Entry.ViewedAt)
	}
	if len(got.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", got.Warnings)
	}
}

func TestAnalyzeCacheMissCallsGitHubThenAIThenStore(t *testing.T) {
	meta := &core.RepoMeta{FullName: "owner/repo"}
	s, gh, ai := &fakeStore{}, &fakeGitHub{data: &clients.RepositoryData{Meta: meta, README: "readme"}}, &fakeAI{analysis: &core.Analysis{Summary: "new"}}
	got, err := newAnalyzer(s, gh, ai).Analyze(context.Background(), "owner/repo", "ja", "claude", false)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if gh.calls != 1 || ai.calls != 1 || s.upsertCalls != 1 {
		t.Fatalf("calls github=%d ai=%d upsert=%d", gh.calls, ai.calls, s.upsertCalls)
	}
	if got.Entry.Analyses["ja"] != ai.analysis || got.Entry.RepoMeta != meta {
		t.Fatalf("unexpected entry: %#v", got.Entry)
	}
	if !got.Entry.RepoMeta.FetchedAt.Equal(time.Unix(99, 0)) {
		t.Fatalf("FetchedAt not set on full fetch: %v", got.Entry.RepoMeta.FetchedAt)
	}
}

func TestAnalyzeForceIgnoresCache(t *testing.T) {
	entry := &core.Entry{FullName: "owner/repo", Analyses: map[string]*core.Analysis{"ja": {Summary: "old"}}}
	s := &fakeStore{entries: []*core.Entry{entry}}
	gh := &fakeGitHub{data: &clients.RepositoryData{Meta: &core.RepoMeta{FullName: "owner/repo"}}}
	ai := &fakeAI{analysis: &core.Analysis{Summary: "new"}}
	got, err := newAnalyzer(s, gh, ai).Analyze(context.Background(), "owner/repo", "ja", "claude", true)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if gh.calls != 1 || ai.calls != 1 || got.Entry.Analyses["ja"].Summary != "new" {
		t.Fatalf("force re-generation failed: %#v", got.Entry)
	}
}

func TestAnalyzeForcePreservesNote(t *testing.T) {
	entry := &core.Entry{FullName: "owner/repo", Note: "学習メモ", Analyses: map[string]*core.Analysis{"ja": {Summary: "old"}}}
	s := &fakeStore{entries: []*core.Entry{entry}}
	gh := &fakeGitHub{data: &clients.RepositoryData{Meta: &core.RepoMeta{FullName: "owner/repo"}}}
	ai := &fakeAI{analysis: &core.Analysis{Summary: "new"}}
	got, err := newAnalyzer(s, gh, ai).Analyze(context.Background(), "owner/repo", "ja", "claude", true)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.Entry.Note != "学習メモ" {
		t.Fatalf("Note = %q, want 学習メモ preserved on force re-analysis", got.Entry.Note)
	}
}

func TestAnalyzeCacheHitPreservesNote(t *testing.T) {
	entry := &core.Entry{FullName: "owner/repo", Note: "学習メモ", RepoMeta: &core.RepoMeta{FetchedAt: time.Unix(99, 0)}, Analyses: map[string]*core.Analysis{"ja": {PromptVersion: 1, Provider: "claude", Summary: "cached"}}}
	s, gh, ai := &fakeStore{entries: []*core.Entry{entry}}, &fakeGitHub{}, &fakeAI{}
	got, err := newAnalyzer(s, gh, ai).Analyze(context.Background(), "owner/repo", "ja", "claude", false)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.Entry.Note != "学習メモ" {
		t.Fatalf("Note = %q, want 学習メモ preserved on cache hit", got.Entry.Note)
	}
}

func TestAnalyzeNewEntryHasEmptyNote(t *testing.T) {
	meta := &core.RepoMeta{FullName: "owner/repo"}
	s, gh, ai := &fakeStore{}, &fakeGitHub{data: &clients.RepositoryData{Meta: meta, README: "readme"}}, &fakeAI{analysis: &core.Analysis{}}
	got, err := newAnalyzer(s, gh, ai).Analyze(context.Background(), "owner/repo", "ja", "claude", false)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.Entry.Note != "" {
		t.Fatalf("Note = %q, want empty for new entry", got.Entry.Note)
	}
}

func TestAnalyzePassesFormattedCodeContextToAI(t *testing.T) {
	meta := &core.RepoMeta{FullName: "owner/repo"}
	data := &clients.RepositoryData{
		Meta:   meta,
		README: "readme",
		Code: &clients.CodeContext{Files: []clients.CodeFile{
			{Path: "go.mod", Content: "module example"},
			{Path: "main.go", Content: "package main"},
		}},
	}
	s, gh, ai := &fakeStore{}, &fakeGitHub{data: data}, &fakeAI{analysis: &core.Analysis{}}
	_, err := newAnalyzer(s, gh, ai).Analyze(context.Background(), "owner/repo", "ja", "claude", false)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	want := "go.mod:\nmodule example\nmain.go:\npackage main"
	if ai.code != want {
		t.Fatalf("code passed to AI = %q, want %q", ai.code, want)
	}
}

func TestAnalyzePassesEmptyCodeWhenContextIsNil(t *testing.T) {
	s, gh, ai := &fakeStore{}, &fakeGitHub{data: &clients.RepositoryData{Meta: &core.RepoMeta{FullName: "owner/repo"}, README: "readme"}}, &fakeAI{analysis: &core.Analysis{}}
	_, err := newAnalyzer(s, gh, ai).Analyze(context.Background(), "owner/repo", "ja", "claude", false)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if ai.code != "" {
		t.Fatalf("code = %q, want empty when Code context is nil", ai.code)
	}
}

func TestAnalyzeCacheMatchesOnVersionProviderAndModel(t *testing.T) {
	entry := &core.Entry{FullName: "owner/repo", RepoMeta: &core.RepoMeta{FetchedAt: time.Unix(99, 0)}, Analyses: map[string]*core.Analysis{"ja": {PromptVersion: 1, Provider: "claude", Model: "m"}}}
	s, gh, ai := &fakeStore{entries: []*core.Entry{entry}}, &fakeGitHub{}, &fakeAI{}
	_, err := newAnalyzer(s, gh, ai).Analyze(context.Background(), "owner/repo", "ja", "claude", false)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if gh.calls != 0 || ai.calls != 0 || gh.metaCalls != 0 {
		t.Fatalf("same version+provider+model must hit cache, calls github=%d ai=%d meta=%d", gh.calls, ai.calls, gh.metaCalls)
	}
}

func TestAnalyzeCacheMissesOnProviderMismatch(t *testing.T) {
	entry := &core.Entry{FullName: "owner/repo", Analyses: map[string]*core.Analysis{"ja": {PromptVersion: 1, Provider: "openai"}}}
	s := &fakeStore{entries: []*core.Entry{entry}}
	gh := &fakeGitHub{data: &clients.RepositoryData{Meta: &core.RepoMeta{FullName: "owner/repo"}}}
	ai := &fakeAI{analysis: &core.Analysis{}}
	_, err := newAnalyzer(s, gh, ai).Analyze(context.Background(), "owner/repo", "ja", "claude", false)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if gh.calls != 1 || ai.calls != 1 {
		t.Fatalf("provider mismatch must regenerate, calls github=%d ai=%d", gh.calls, ai.calls)
	}
}

func TestAnalyzeCacheMissesOnVersionMismatch(t *testing.T) {
	entry := &core.Entry{FullName: "owner/repo", Analyses: map[string]*core.Analysis{"ja": {PromptVersion: 0, Provider: "claude"}}}
	s := &fakeStore{entries: []*core.Entry{entry}}
	gh := &fakeGitHub{data: &clients.RepositoryData{Meta: &core.RepoMeta{FullName: "owner/repo"}}}
	ai := &fakeAI{analysis: &core.Analysis{}}
	_, err := newAnalyzer(s, gh, ai).Analyze(context.Background(), "owner/repo", "ja", "claude", false)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if gh.calls != 1 || ai.calls != 1 {
		t.Fatalf("version mismatch (existing analysis) must regenerate once, calls github=%d ai=%d", gh.calls, ai.calls)
	}
}

func TestAnalyzeRejectsInputWithoutExternalCalls(t *testing.T) {
	s, gh, ai := &fakeStore{}, &fakeGitHub{}, &fakeAI{}
	_, err := newAnalyzer(s, gh, ai).Analyze(context.Background(), "invalid", "ja", "claude", false)
	if err == nil || s.loadCalls != 0 || gh.calls != 0 || ai.calls != 0 {
		t.Fatalf("err=%v calls load=%d github=%d ai=%d", err, s.loadCalls, gh.calls, ai.calls)
	}
}

func TestAnalyzeCancellationStopsBeforeExternalCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s, gh, ai := &fakeStore{}, &fakeGitHub{}, &fakeAI{}
	_, err := newAnalyzer(s, gh, ai).Analyze(ctx, "owner/repo", "ja", "claude", false)
	if err == nil || s.loadCalls != 0 || gh.calls != 0 || ai.calls != 0 {
		t.Fatalf("err=%v calls load=%d github=%d ai=%d", err, s.loadCalls, gh.calls, ai.calls)
	}
}

func TestAnalyzeHidesSecretsInMappedErrors(t *testing.T) {
	t.Run("load", func(t *testing.T) {
		_, err := newAnalyzer(&fakeStore{loadErr: errors.New("secret")}, &fakeGitHub{}, &fakeAI{}).Analyze(context.Background(), "owner/repo", "ja", "claude", false)
		assertSafe(t, err)
	})
	t.Run("github", func(t *testing.T) {
		_, err := newAnalyzer(&fakeStore{}, &fakeGitHub{err: errors.New("secret")}, &fakeAI{}).Analyze(context.Background(), "owner/repo", "ja", "claude", false)
		assertSafe(t, err)
	})
	t.Run("ai", func(t *testing.T) {
		_, err := newAnalyzer(&fakeStore{}, &fakeGitHub{data: &clients.RepositoryData{Meta: &core.RepoMeta{}}}, &fakeAI{err: errors.New("secret")}).Analyze(context.Background(), "owner/repo", "ja", "claude", false)
		assertSafe(t, err)
	})
	t.Run("upsert", func(t *testing.T) {
		_, err := newAnalyzer(&fakeStore{upsertErr: errors.New("secret")}, &fakeGitHub{data: &clients.RepositoryData{Meta: &core.RepoMeta{}}}, &fakeAI{analysis: &core.Analysis{}}).Analyze(context.Background(), "owner/repo", "ja", "claude", false)
		assertSafe(t, err)
	})
}

func assertSafe(t *testing.T, err error) {
	t.Helper()
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func cachedEntry(fetchedAt time.Time) *core.Entry {
	return &core.Entry{
		FullName: "owner/repo",
		RepoMeta: &core.RepoMeta{FullName: "owner/repo", Description: "old", Stars: 1, UpdatedAt: time.Unix(10, 0), Languages: map[string]int{"Go": 100}, FetchedAt: fetchedAt},
		Analyses: map[string]*core.Analysis{"ja": {Summary: "cached", PromptVersion: 1, Provider: "claude", CreatedAt: time.Unix(5, 0)}},
	}
}

func TestAnalyzeRefreshNotNeededWhenWithinInterval(t *testing.T) {
	entry := cachedEntry(time.Unix(99, 0))
	s, gh, ai := &fakeStore{entries: []*core.Entry{entry}}, &fakeGitHub{}, &fakeAI{}
	got, err := newAnalyzer(s, gh, ai).Analyze(context.Background(), "owner/repo", "ja", "claude", false)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if gh.metaCalls != 0 {
		t.Fatalf("meta refresh calls = %d, want 0 within interval", gh.metaCalls)
	}
	if got.Entry.RepoMeta.FetchedAt.Unix() != 99 {
		t.Fatalf("FetchedAt changed without refresh: %v", got.Entry.RepoMeta.FetchedAt)
	}
	if !got.Entry.ViewedAt.Equal(time.Unix(99, 0)) {
		t.Fatalf("ViewedAt not updated: %v", got.Entry.ViewedAt)
	}
}

func TestAnalyzeRefreshFetchesMetaOnceWhenIntervalElapsed(t *testing.T) {
	entry := cachedEntry(time.Unix(98, 0))
	s, gh, ai := &fakeStore{entries: []*core.Entry{entry}}, &fakeGitHub{meta: &core.RepoMeta{UpdatedAt: time.Unix(10, 0)}}, &fakeAI{}
	got, err := newAnalyzer(s, gh, ai).Analyze(context.Background(), "owner/repo", "ja", "claude", false)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if gh.metaCalls != 1 {
		t.Fatalf("meta refresh calls = %d, want 1", gh.metaCalls)
	}
	if gh.calls != 0 || ai.calls != 0 {
		t.Fatalf("refresh must not re-fetch README/code or re-generate: github=%d ai=%d", gh.calls, ai.calls)
	}
	if !got.Entry.RepoMeta.FetchedAt.Equal(time.Unix(99, 0)) {
		t.Fatalf("FetchedAt not updated after refresh: %v", got.Entry.RepoMeta.FetchedAt)
	}
	if got.Entry.Analyses["ja"].Summary != "cached" {
		t.Fatalf("analysis was re-generated: %#v", got.Entry.Analyses["ja"])
	}
}

func TestAnalyzeRefreshWhenFetchedAtZero(t *testing.T) {
	entry := cachedEntry(time.Time{})
	s, gh, ai := &fakeStore{entries: []*core.Entry{entry}}, &fakeGitHub{meta: &core.RepoMeta{UpdatedAt: time.Unix(10, 0)}}, &fakeAI{}
	_, err := newAnalyzer(s, gh, ai).Analyze(context.Background(), "owner/repo", "ja", "claude", false)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if gh.metaCalls != 1 {
		t.Fatalf("meta refresh calls = %d, want 1 for zero FetchedAt", gh.metaCalls)
	}
}

func TestAnalyzeRefreshUpdatesScalarsAndKeepsLanguages(t *testing.T) {
	entry := cachedEntry(time.Unix(98, 0))
	fresh := &core.RepoMeta{FullName: "owner/repo", Description: "new", Stars: 42, Forks: 7, Language: "Go", Topics: []string{"tui"}, URL: "https://github.com/owner/repo", License: "MIT", UpdatedAt: time.Unix(20, 0)}
	s, gh, ai := &fakeStore{entries: []*core.Entry{entry}}, &fakeGitHub{meta: fresh}, &fakeAI{}
	got, err := newAnalyzer(s, gh, ai).Analyze(context.Background(), "owner/repo", "ja", "claude", false)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	meta := got.Entry.RepoMeta
	if meta.Description != "new" || meta.Stars != 42 || meta.Forks != 7 || !meta.UpdatedAt.Equal(time.Unix(20, 0)) || meta.License != "MIT" {
		t.Fatalf("scalars not merged: %#v", meta)
	}
	if !reflect.DeepEqual(meta.Languages, map[string]int{"Go": 100}) {
		t.Fatalf("Languages lost on merge: %#v", meta.Languages)
	}
}

func TestAnalyzeRefreshSameUpdatedAtOnlyRenewsFetchedAt(t *testing.T) {
	entry := cachedEntry(time.Unix(98, 0))
	s, gh, ai := &fakeStore{entries: []*core.Entry{entry}}, &fakeGitHub{meta: &core.RepoMeta{FullName: "owner/repo", Description: "different", UpdatedAt: time.Unix(10, 0)}}, &fakeAI{}
	got, err := newAnalyzer(s, gh, ai).Analyze(context.Background(), "owner/repo", "ja", "claude", false)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.Entry.RepoMeta.Description != "old" {
		t.Fatalf("scalars replaced despite same updated_at: %#v", got.Entry.RepoMeta)
	}
	if !got.Entry.RepoMeta.FetchedAt.Equal(time.Unix(99, 0)) {
		t.Fatalf("FetchedAt not renewed: %v", got.Entry.RepoMeta.FetchedAt)
	}
}

func TestAnalyzeRefreshFailureWarnsAndKeepsCache(t *testing.T) {
	entry := cachedEntry(time.Unix(98, 0))
	s, gh, ai := &fakeStore{entries: []*core.Entry{entry}}, &fakeGitHub{metaErr: errors.New("boom")}, &fakeAI{}
	got, err := newAnalyzer(s, gh, ai).Analyze(context.Background(), "owner/repo", "ja", "claude", false)
	if err != nil {
		t.Fatalf("refresh failure must not fail viewing: %v", err)
	}
	if got.Entry.Analyses["ja"].Summary != "cached" {
		t.Fatalf("cached entry lost on refresh failure: %#v", got.Entry.Analyses["ja"])
	}
	if len(got.Warnings) == 0 {
		t.Fatalf("refresh failure must add a warning")
	}
}

func TestAnalyzeRefreshFailureMapsContextCancellationToError(t *testing.T) {
	entry := cachedEntry(time.Unix(98, 0))
	ctx, cancel := context.WithCancel(context.Background())
	gh := &fakeGitHub{metaCancel: cancel, metaErr: errors.New("canceled")}
	s, ai := &fakeStore{entries: []*core.Entry{entry}}, &fakeAI{}
	_, err := newAnalyzer(s, gh, ai).Analyze(ctx, "owner/repo", "ja", "claude", false)
	if err == nil {
		t.Fatal("cancellation during refresh must return an error")
	}
	if s.upsertCalls != 0 {
		t.Fatalf("upsert after cancellation: %d", s.upsertCalls)
	}
}

func TestNeedsRefresh(t *testing.T) {
	now := time.Unix(99, 0)
	if !needsRefresh(nil, now, time.Second) {
		t.Fatal("nil meta must need refresh")
	}
	if !needsRefresh(&core.RepoMeta{}, now, time.Second) {
		t.Fatal("zero FetchedAt must need refresh")
	}
	if needsRefresh(&core.RepoMeta{FetchedAt: time.Unix(99, 0)}, now, time.Second) {
		t.Fatal("within interval must not need refresh")
	}
	if !needsRefresh(&core.RepoMeta{FetchedAt: time.Unix(98, 0)}, now, time.Second) {
		t.Fatal("elapsed interval must need refresh")
	}
}

func TestMergeMetaKeepsLanguagesAndFetchedAt(t *testing.T) {
	old := &core.RepoMeta{FullName: "owner/repo", Description: "old", Languages: map[string]int{"Go": 1}, FetchedAt: time.Unix(1, 0)}
	fresh := &core.RepoMeta{FullName: "owner/repo", Description: "new", Stars: 5, UpdatedAt: time.Unix(2, 0)}
	merged := mergeMeta(old, fresh)
	if merged.Description != "new" || merged.Stars != 5 {
		t.Fatalf("scalars not merged: %#v", merged)
	}
	if !reflect.DeepEqual(merged.Languages, map[string]int{"Go": 1}) {
		t.Fatalf("Languages not kept: %#v", merged.Languages)
	}
	if !merged.FetchedAt.Equal(time.Unix(1, 0)) {
		t.Fatalf("FetchedAt not kept: %v", merged.FetchedAt)
	}
	if old.Description != "old" {
		t.Fatal("mergeMeta mutated the source")
	}
}
