package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/issy20/reporepo/internal/clients"
	"github.com/issy20/reporepo/internal/core"
)

type recordingStore struct {
	entries                     []*core.Entry
	loadErr, saveErr, upsertErr error
	saveCalls, upsertCalls      int
}

func (s *recordingStore) Load() ([]*core.Entry, error) { return s.entries, s.loadErr }
func (s *recordingStore) Save(e []*core.Entry) error   { s.saveCalls++; s.entries = e; return s.saveErr }
func (s *recordingStore) Upsert(e *core.Entry) error {
	s.upsertCalls++
	if s.upsertErr == nil {
		s.entries = []*core.Entry{e}
	}
	return s.upsertErr
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
	cancel   context.CancelFunc
}

func (f *fakeAI) Generate(context.Context, *core.RepoMeta, string, string) (*core.Analysis, error) {
	f.calls++
	if f.cancel != nil {
		f.cancel()
	}
	return f.analysis, f.err
}

func commandModel(store entryStore, gh clients.GitHubClient, ai clients.AIClient) Model {
	return NewModel(Dependencies{Store: store, GitHub: gh, AI: map[string]clients.AIClient{"claude": ai}, Now: func() time.Time { return time.Unix(99, 0) }}, nil)
}

func TestAnalyzeRejectsInvalidInputWithoutSideEffects(t *testing.T) {
	s, gh, ai := &recordingStore{}, &fakeGitHub{}, &fakeAI{}
	_, err := commandModel(s, gh, ai).analyze(context.Background(), "invalid", false)
	if err == nil || gh.calls != 0 || ai.calls != 0 || s.upsertCalls != 0 {
		t.Fatalf("err=%v calls=%d/%d/%d", err, gh.calls, ai.calls, s.upsertCalls)
	}
}

func TestAnalyzeUsesCacheAndUpdatesViewedAt(t *testing.T) {
	entry := &core.Entry{FullName: "owner/repo", Analyses: map[string]*core.Analysis{"ja": {Summary: "cached"}}}
	s, gh, ai := &recordingStore{entries: []*core.Entry{entry}}, &fakeGitHub{}, &fakeAI{}
	got, err := commandModel(s, gh, ai).analyze(context.Background(), "owner/repo", false)
	if err != nil || got != entry || gh.calls != 0 || ai.calls != 0 || s.upsertCalls != 1 || !entry.ViewedAt.Equal(time.Unix(99, 0)) {
		t.Fatalf("got=%#v err=%v calls=%d/%d/%d", got, err, gh.calls, ai.calls, s.upsertCalls)
	}
}

func TestAnalyzeFetchesGeneratesAndPreservesExistingLanguage(t *testing.T) {
	english := &core.Analysis{Summary: "English"}
	existing := &core.Entry{FullName: "owner/repo", IsFavorite: true, CreatedAt: time.Unix(1, 0), Analyses: map[string]*core.Analysis{"en": english}}
	meta := &core.RepoMeta{FullName: "owner/repo"}
	generated := &core.Analysis{Summary: "日本語"}
	s, gh, ai := &recordingStore{entries: []*core.Entry{existing}}, &fakeGitHub{data: &clients.RepositoryData{Meta: meta, README: "readme"}}, &fakeAI{analysis: generated}
	got, err := commandModel(s, gh, ai).analyze(context.Background(), "owner/repo", false)
	if err != nil || gh.calls != 1 || ai.calls != 1 || s.upsertCalls != 1 {
		t.Fatalf("err=%v calls=%d/%d/%d", err, gh.calls, ai.calls, s.upsertCalls)
	}
	if got.Analyses["en"] != english || got.Analyses["ja"] != generated || !got.IsFavorite || !got.CreatedAt.Equal(time.Unix(1, 0)) {
		t.Fatalf("existing data was lost: %#v", got)
	}
}

func TestAnalyzeForceIgnoresCache(t *testing.T) {
	entry := &core.Entry{FullName: "owner/repo", Analyses: map[string]*core.Analysis{"ja": {Summary: "old"}}}
	s := &recordingStore{entries: []*core.Entry{entry}}
	gh := &fakeGitHub{data: &clients.RepositoryData{Meta: &core.RepoMeta{FullName: "owner/repo"}}}
	ai := &fakeAI{analysis: &core.Analysis{Summary: "new"}}
	got, err := commandModel(s, gh, ai).analyze(context.Background(), "owner/repo", true)
	if err != nil || gh.calls != 1 || ai.calls != 1 || got.Analyses["ja"].Summary != "new" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

func TestAnalyzeRejectsUnknownProviderBeforeExternalCalls(t *testing.T) {
	s, gh := &recordingStore{}, &fakeGitHub{}
	m := commandModel(s, gh, nil)
	m.provider = "unknown"
	_, err := m.analyze(context.Background(), "owner/repo", false)
	if err == nil || gh.calls != 0 || s.upsertCalls != 0 {
		t.Fatalf("err=%v github=%d upsert=%d", err, gh.calls, s.upsertCalls)
	}
}

func TestAnalyzeDoesNotSaveAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &recordingStore{}
	gh := &fakeGitHub{data: &clients.RepositoryData{Meta: &core.RepoMeta{FullName: "owner/repo"}}}
	ai := &fakeAI{analysis: &core.Analysis{Summary: "x"}, cancel: cancel}
	_, err := commandModel(s, gh, ai).analyze(ctx, "owner/repo", false)
	if err == nil || s.upsertCalls != 0 {
		t.Fatalf("err=%v upsert=%d", err, s.upsertCalls)
	}
}

func TestAnalyzeMapsDependencyErrors(t *testing.T) {
	t.Run("load", func(t *testing.T) {
		m := commandModel(&recordingStore{loadErr: errors.New("secret")}, &fakeGitHub{}, &fakeAI{})
		if _, err := m.analyze(context.Background(), "owner/repo", false); err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("github", func(t *testing.T) {
		m := commandModel(&recordingStore{}, &fakeGitHub{err: errors.New("secret")}, &fakeAI{})
		if _, err := m.analyze(context.Background(), "owner/repo", false); err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("ai", func(t *testing.T) {
		m := commandModel(&recordingStore{}, &fakeGitHub{data: &clients.RepositoryData{Meta: &core.RepoMeta{FullName: "owner/repo"}}}, &fakeAI{err: errors.New("secret")})
		if _, err := m.analyze(context.Background(), "owner/repo", false); err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("upsert", func(t *testing.T) {
		m := commandModel(&recordingStore{upsertErr: errors.New("secret")}, &fakeGitHub{data: &clients.RepositoryData{Meta: &core.RepoMeta{FullName: "owner/repo"}}}, &fakeAI{analysis: &core.Analysis{}})
		if _, err := m.analyze(context.Background(), "owner/repo", false); err == nil {
			t.Fatal("want error")
		}
	})
}
