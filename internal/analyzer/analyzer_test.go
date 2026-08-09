package analyzer

import (
	"context"
	"errors"
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
	calls int
	data  *clients.RepositoryData
	err   error
}

func (f *fakeGitHub) FetchRepository(context.Context, string, string) (*clients.RepositoryData, error) {
	f.calls++
	return f.data, f.err
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
	return New(s, gh, map[string]clients.AIClient{"claude": ai}, func() time.Time { return time.Unix(99, 0) })
}

func TestAnalyzeCacheHitDoesNotCallExternalServices(t *testing.T) {
	entry := &core.Entry{FullName: "owner/repo", Analyses: map[string]*core.Analysis{"ja": {Summary: "cached", PromptVersion: 1, Provider: "claude"}}}
	s, gh, ai := &fakeStore{entries: []*core.Entry{entry}}, &fakeGitHub{}, &fakeAI{}
	got, err := newAnalyzer(s, gh, ai).Analyze(context.Background(), "owner/repo", "ja", "claude", false)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if gh.calls != 0 || ai.calls != 0 || s.upsertCalls != 1 {
		t.Fatalf("calls github=%d ai=%d upsert=%d", gh.calls, ai.calls, s.upsertCalls)
	}
	if got.ViewedAt != s.upserted[0].ViewedAt || !got.ViewedAt.Equal(time.Unix(99, 0)) {
		t.Fatalf("ViewedAt not updated: %v", got.ViewedAt)
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
	if got.Analyses["ja"] != ai.analysis || got.RepoMeta != meta {
		t.Fatalf("unexpected entry: %#v", got)
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
	if gh.calls != 1 || ai.calls != 1 || got.Analyses["ja"].Summary != "new" {
		t.Fatalf("force re-generation failed: %#v", got)
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
	entry := &core.Entry{FullName: "owner/repo", Analyses: map[string]*core.Analysis{"ja": {PromptVersion: 1, Provider: "claude", Model: "m"}}}
	s, gh, ai := &fakeStore{entries: []*core.Entry{entry}}, &fakeGitHub{}, &fakeAI{}
	_, err := newAnalyzer(s, gh, ai).Analyze(context.Background(), "owner/repo", "ja", "claude", false)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if gh.calls != 0 || ai.calls != 0 {
		t.Fatalf("same version+provider+model must hit cache, calls github=%d ai=%d", gh.calls, ai.calls)
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
