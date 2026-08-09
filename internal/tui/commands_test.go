package tui

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

type recordingStore struct {
	entries                           []*core.Entry
	loadErr, saveErr, upsertErr       error
	loadCalls, saveCalls, upsertCalls int
	events                            *[]string
}

func (s *recordingStore) Load() ([]*core.Entry, error) { s.loadCalls++; return s.entries, s.loadErr }
func (s *recordingStore) Save(e []*core.Entry) error   { s.saveCalls++; s.entries = e; return s.saveErr }
func (s *recordingStore) Upsert(e *core.Entry) error {
	s.upsertCalls++
	if s.events != nil {
		*s.events = append(*s.events, "store")
	}
	if s.upsertErr == nil {
		for i, entry := range s.entries {
			if entry != nil && strings.EqualFold(entry.FullName, e.FullName) {
				s.entries[i] = e
				return nil
			}
		}
		s.entries = append(s.entries, e)
	}
	return s.upsertErr
}

type fakeGitHub struct {
	calls       int
	data        *clients.RepositoryData
	err         error
	owner, repo string
	cancel      context.CancelFunc
	events      *[]string
}

func (f *fakeGitHub) FetchRepository(_ context.Context, owner, repo string) (*clients.RepositoryData, error) {
	f.calls++
	f.owner, f.repo = owner, repo
	if f.events != nil {
		*f.events = append(*f.events, "github")
	}
	if f.cancel != nil {
		f.cancel()
	}
	return f.data, f.err
}

type fakeAI struct {
	calls    int
	analysis *core.Analysis
	err      error
	cancel   context.CancelFunc
	meta     *core.RepoMeta
	readme   string
	language string
	events   *[]string
}

func (f *fakeAI) Generate(_ context.Context, meta *core.RepoMeta, readme, code, language string) (*core.Analysis, error) {
	f.calls++
	f.meta, f.readme, f.language = meta, readme, language
	if f.events != nil {
		*f.events = append(*f.events, "ai")
	}
	if f.cancel != nil {
		f.cancel()
	}
	return f.analysis, f.err
}

func commandModel(store entryStore, gh clients.GitHubClient, ai clients.AIClient) Model {
	m := NewModel(Dependencies{Store: store, GitHub: gh, AI: map[string]clients.AIClient{"claude": ai}, Now: func() time.Time { return time.Unix(99, 0) }}, nil)
	if recording, ok := store.(*recordingStore); ok {
		recording.loadCalls = 0
	}
	return m
}

func TestAnalyzeRejectsInvalidInputWithoutSideEffects(t *testing.T) {
	s, gh, ai := &recordingStore{}, &fakeGitHub{}, &fakeAI{}
	_, err := commandModel(s, gh, ai).analyze(context.Background(), "invalid", false)
	if err == nil || s.loadCalls != 0 || gh.calls != 0 || ai.calls != 0 || s.upsertCalls != 0 {
		t.Fatalf("err=%v calls=%d/%d/%d/%d", err, s.loadCalls, gh.calls, ai.calls, s.upsertCalls)
	}
}

func TestAnalyzeCacheLookupIgnoresFullNameCaseAndNilAnalysesMisses(t *testing.T) {
	t.Run("case insensitive cache hit", func(t *testing.T) {
		s := &recordingStore{entries: []*core.Entry{{FullName: "Owner/Repo", Analyses: map[string]*core.Analysis{"ja": {PromptVersion: 1, Provider: "claude"}}}}}
		_, err := commandModel(s, &fakeGitHub{}, &fakeAI{}).analyze(context.Background(), "owner/repo", false)
		if err != nil || s.upsertCalls != 1 {
			t.Fatalf("err=%v upsert=%d", err, s.upsertCalls)
		}
	})
	t.Run("nil analyses is cache miss", func(t *testing.T) {
		s := &recordingStore{entries: []*core.Entry{{FullName: "owner/repo"}}}
		gh := &fakeGitHub{data: &clients.RepositoryData{Meta: &core.RepoMeta{FullName: "owner/repo"}}}
		ai := &fakeAI{analysis: &core.Analysis{}}
		_, err := commandModel(s, gh, ai).analyze(context.Background(), "owner/repo", false)
		if err != nil || gh.calls != 1 || ai.calls != 1 {
			t.Fatalf("err=%v calls=%d/%d", err, gh.calls, ai.calls)
		}
	})
}

func TestAnalyzeCancellationBeforeWorkSkipsDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s, gh, ai := &recordingStore{}, &fakeGitHub{}, &fakeAI{}
	_, err := commandModel(s, gh, ai).analyze(ctx, "owner/repo", false)
	if err == nil || s.loadCalls != 0 || gh.calls != 0 || ai.calls != 0 || s.upsertCalls != 0 {
		t.Fatalf("err=%v calls=%d/%d/%d/%d", err, s.loadCalls, gh.calls, ai.calls, s.upsertCalls)
	}
}

func TestAnalyzeUsesCacheAndUpdatesViewedAt(t *testing.T) {
	entry := &core.Entry{FullName: "owner/repo", Analyses: map[string]*core.Analysis{"ja": {Summary: "cached", PromptVersion: 1, Provider: "claude"}}}
	s, gh, ai := &recordingStore{entries: []*core.Entry{entry}}, &fakeGitHub{}, &fakeAI{}
	got, err := commandModel(s, gh, ai).analyze(context.Background(), "owner/repo", false)
	if err != nil || got == entry || gh.calls != 0 || ai.calls != 0 || s.upsertCalls != 1 || !got.ViewedAt.Equal(time.Unix(99, 0)) || !entry.ViewedAt.IsZero() {
		t.Fatalf("got=%#v err=%v calls=%d/%d/%d", got, err, gh.calls, ai.calls, s.upsertCalls)
	}
}

func TestAnalyzeCacheUpsertFailureDoesNotMutateLoadedEntry(t *testing.T) {
	originalViewedAt := time.Unix(1, 0)
	entry := &core.Entry{
		FullName: "owner/repo",
		ViewedAt: originalViewedAt,
		Analyses: map[string]*core.Analysis{"ja": {Summary: "cached", PromptVersion: 1, Provider: "claude"}},
	}
	s := &recordingStore{entries: []*core.Entry{entry}, upsertErr: errors.New("save failed")}

	got, err := commandModel(s, &fakeGitHub{}, &fakeAI{}).analyze(context.Background(), "owner/repo", false)

	if err == nil {
		t.Fatal("want error")
	}
	if got != nil {
		t.Fatalf("got=%#v, want nil", got)
	}
	if !entry.ViewedAt.Equal(originalViewedAt) {
		t.Fatalf("loaded entry ViewedAt=%v, want %v", entry.ViewedAt, originalViewedAt)
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

func TestAnalyzePassesArgumentsInDependencyOrder(t *testing.T) {
	events := []string{}
	meta := &core.RepoMeta{FullName: "owner/repo"}
	s := &recordingStore{events: &events}
	gh := &fakeGitHub{data: &clients.RepositoryData{Meta: meta, README: "README body"}, events: &events}
	ai := &fakeAI{analysis: &core.Analysis{}, events: &events}
	_, err := commandModel(s, gh, ai).analyze(context.Background(), "owner/repo", false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events, []string{"github", "ai", "store"}) {
		t.Fatalf("events=%v", events)
	}
	if gh.owner != "owner" || gh.repo != "repo" {
		t.Fatalf("github args=%q/%q", gh.owner, gh.repo)
	}
	if ai.meta != meta || ai.readme != "README body" || ai.language != "ja" {
		t.Fatalf("ai args=%#v %q %q", ai.meta, ai.readme, ai.language)
	}
}

func TestAnalyzeCreatesCompleteEntryAndFallsBackToInputFullName(t *testing.T) {
	meta := &core.RepoMeta{}
	analysis := &core.Analysis{Summary: "generated"}
	s := &recordingStore{}
	got, err := commandModel(s, &fakeGitHub{data: &clients.RepositoryData{Meta: meta}}, &fakeAI{analysis: analysis}).analyze(context.Background(), "Owner/Repo", false)
	wantTime := time.Unix(99, 0)
	if err != nil || got.FullName != "Owner/Repo" || got.RepoMeta != meta || got.Analyses["ja"] != analysis || !got.CreatedAt.Equal(wantTime) || !got.ViewedAt.Equal(wantTime) {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

func TestAnalyzeSaveFailureDoesNotMutateExistingEntry(t *testing.T) {
	oldMeta := &core.RepoMeta{FullName: "owner/repo", Description: "old"}
	oldAnalysis := &core.Analysis{Summary: "old"}
	existing := &core.Entry{FullName: "owner/repo", RepoMeta: oldMeta, Analyses: map[string]*core.Analysis{"en": oldAnalysis}, ViewedAt: time.Unix(1, 0)}
	s := &recordingStore{entries: []*core.Entry{existing}, upsertErr: errors.New("secret")}
	_, err := commandModel(s, &fakeGitHub{data: &clients.RepositoryData{Meta: &core.RepoMeta{FullName: "owner/repo", Description: "new"}}}, &fakeAI{analysis: &core.Analysis{Summary: "new"}}).analyze(context.Background(), "owner/repo", false)
	if err == nil || existing.RepoMeta != oldMeta || existing.Analyses["en"] != oldAnalysis || existing.Analyses["ja"] != nil || !existing.ViewedAt.Equal(time.Unix(1, 0)) {
		t.Fatalf("entry mutated: %#v err=%v", existing, err)
	}
}

func TestAnalyzeRejectsNilDependenciesAndResponses(t *testing.T) {
	tests := []struct {
		name           string
		model          Model
		wantGH, wantAI int
	}{
		{"nil ai map", NewModel(Dependencies{Store: &recordingStore{}, GitHub: &fakeGitHub{}}, nil), 0, 0},
		{"nil github", commandModel(&recordingStore{}, nil, &fakeAI{}), 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.model.analyze(context.Background(), "owner/repo", false); err == nil {
				t.Fatal("want error")
			}
		})
	}

	for _, tc := range []struct {
		name string
		data *clients.RepositoryData
	}{{"nil data", nil}, {"nil metadata", &clients.RepositoryData{}}} {
		t.Run(tc.name, func(t *testing.T) {
			ai := &fakeAI{analysis: &core.Analysis{}}
			s := &recordingStore{}
			_, err := commandModel(s, &fakeGitHub{data: tc.data}, ai).analyze(context.Background(), "owner/repo", false)
			if err == nil || ai.calls != 0 || s.upsertCalls != 0 {
				t.Fatalf("err=%v ai=%d upsert=%d", err, ai.calls, s.upsertCalls)
			}
		})
	}
	t.Run("nil analysis", func(t *testing.T) {
		s := &recordingStore{}
		_, err := commandModel(s, &fakeGitHub{data: &clients.RepositoryData{Meta: &core.RepoMeta{}}}, &fakeAI{}).analyze(context.Background(), "owner/repo", false)
		if err == nil || s.upsertCalls != 0 {
			t.Fatalf("err=%v upsert=%d", err, s.upsertCalls)
		}
	})
}

func TestAnalyzeStopsAtCancellationBoundaries(t *testing.T) {
	t.Run("after github", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		ai := &fakeAI{analysis: &core.Analysis{}}
		s := &recordingStore{}
		_, err := commandModel(s, &fakeGitHub{data: &clients.RepositoryData{Meta: &core.RepoMeta{}}, cancel: cancel}, ai).analyze(ctx, "owner/repo", false)
		if err == nil || ai.calls != 0 || s.upsertCalls != 0 {
			t.Fatalf("err=%v ai=%d upsert=%d", err, ai.calls, s.upsertCalls)
		}
	})
	t.Run("after ai", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		s := &recordingStore{}
		_, err := commandModel(s, &fakeGitHub{data: &clients.RepositoryData{Meta: &core.RepoMeta{}}}, &fakeAI{analysis: &core.Analysis{}, cancel: cancel}).analyze(ctx, "owner/repo", false)
		if err == nil || s.upsertCalls != 0 {
			t.Fatalf("err=%v upsert=%d", err, s.upsertCalls)
		}
	})
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
	assertSafe := func(t *testing.T, err error) {
		t.Helper()
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe error: %v", err)
		}
	}
	t.Run("load", func(t *testing.T) {
		m := commandModel(&recordingStore{loadErr: errors.New("secret")}, &fakeGitHub{}, &fakeAI{})
		_, err := m.analyze(context.Background(), "owner/repo", false)
		assertSafe(t, err)
	})
	t.Run("github", func(t *testing.T) {
		m := commandModel(&recordingStore{}, &fakeGitHub{err: errors.New("secret")}, &fakeAI{})
		_, err := m.analyze(context.Background(), "owner/repo", false)
		assertSafe(t, err)
	})
	t.Run("ai", func(t *testing.T) {
		m := commandModel(&recordingStore{}, &fakeGitHub{data: &clients.RepositoryData{Meta: &core.RepoMeta{FullName: "owner/repo"}}}, &fakeAI{err: errors.New("secret")})
		_, err := m.analyze(context.Background(), "owner/repo", false)
		assertSafe(t, err)
	})
	t.Run("upsert", func(t *testing.T) {
		m := commandModel(&recordingStore{upsertErr: errors.New("secret")}, &fakeGitHub{data: &clients.RepositoryData{Meta: &core.RepoMeta{FullName: "owner/repo"}}}, &fakeAI{analysis: &core.Analysis{}})
		_, err := m.analyze(context.Background(), "owner/repo", false)
		assertSafe(t, err)
	})
}

func TestAnalyzeCmdCarriesRequestIDForSuccessAndFailure(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		m := commandModel(&recordingStore{}, &fakeGitHub{data: &clients.RepositoryData{Meta: &core.RepoMeta{}}}, &fakeAI{analysis: &core.Analysis{}})
		msg, ok := m.analyzeCmd(context.Background(), "owner/repo", false, 42)().(analysisSucceededMsg)
		if !ok || msg.requestID != 42 || msg.entry == nil {
			t.Fatalf("msg=%#v", msg)
		}
	})
	t.Run("failure", func(t *testing.T) {
		m := commandModel(&recordingStore{}, &fakeGitHub{}, &fakeAI{})
		msg, ok := m.analyzeCmd(context.Background(), "invalid", false, 43)().(analysisFailedMsg)
		if !ok || msg.requestID != 43 || msg.err == nil {
			t.Fatalf("msg=%#v", msg)
		}
	})
}
