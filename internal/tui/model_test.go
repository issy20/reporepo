package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/issy20/reporepo/internal/clients"
	"github.com/issy20/reporepo/internal/core"
)

type fakeStore struct {
	entries []*core.Entry
	loadErr error
}

func TestNewModelLoadsHistoryNewestFirstAndSkipsNil(t *testing.T) {
	old := &core.Entry{FullName: "old/repo", ViewedAt: time.Unix(1, 0)}
	newest := &core.Entry{FullName: "new/repo", ViewedAt: time.Unix(2, 0)}
	m := NewModel(Dependencies{Store: &fakeStore{entries: []*core.Entry{old, nil, newest}}}, nil)

	if len(m.entries) != 2 || m.entries[0] != newest || m.entries[1] != old {
		t.Fatalf("entries = %#v, want newest first without nil", m.entries)
	}
}

func TestNewModelKeepsLoadErrorForUser(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{loadErr: errors.New("broken")}}, nil)
	if !strings.Contains(m.errMessage, "履歴を読み込めません") {
		t.Fatalf("errMessage = %q", m.errMessage)
	}
}

func TestRefreshVisibleFiltersFavoritesAndClampsSelection(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{entries: []*core.Entry{
		{FullName: "a/a"}, {FullName: "b/b", IsFavorite: true},
	}}}, nil)
	m.selected = 8
	m.tab = tabFavorites
	m.refreshVisible()

	if len(m.visible) != 1 || m.visible[0].FullName != "b/b" || m.selected != 0 {
		t.Fatalf("visible=%#v selected=%d", m.visible, m.selected)
	}
}

func (s *fakeStore) Load() ([]*core.Entry, error) { return s.entries, s.loadErr }
func (s *fakeStore) Save(entries []*core.Entry) error {
	s.entries = entries
	return nil
}
func (s *fakeStore) Upsert(entry *core.Entry) error {
	s.entries = append(s.entries, entry)
	return nil
}
func (s *fakeStore) Delete(fullName string) error {
	filtered := s.entries[:0]
	for _, e := range s.entries {
		if e != nil && !strings.EqualFold(e.FullName, fullName) {
			filtered = append(filtered, e)
		}
	}
	s.entries = filtered
	return nil
}

func TestNewModelUsesDefaults(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, &core.Config{})

	if m.state != stateInput {
		t.Fatalf("state = %v, want stateInput", m.state)
	}
	if m.language != "ja" {
		t.Errorf("language = %q, want ja", m.language)
	}
	if m.provider != "claude" {
		t.Errorf("provider = %q, want claude", m.provider)
	}
}

func TestNewModelUsesDefaultsWithNilConfig(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)

	if m.state != stateInput {
		t.Fatalf("state = %v, want stateInput", m.state)
	}
	if m.language != "ja" {
		t.Errorf("language = %q, want ja", m.language)
	}
	if m.provider != "claude" {
		t.Errorf("provider = %q, want claude", m.provider)
	}
}

func TestNewModelUsesSupportedConfig(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, &core.Config{
		DefaultLanguage: "en",
		DefaultProvider: "openai",
	})

	if m.language != "en" {
		t.Errorf("language = %q, want en", m.language)
	}
	if m.provider != "openai" {
		t.Errorf("provider = %q, want openai", m.provider)
	}
}

func TestNewModelFallsBackForUnsupportedLanguage(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, &core.Config{
		DefaultLanguage: "fr",
		DefaultProvider: "openai",
	})

	if m.language != "ja" {
		t.Errorf("language = %q, want ja", m.language)
	}
	if m.provider != "openai" {
		t.Errorf("provider = %q, want openai", m.provider)
	}
}

func TestNewModelAcceptsGeminiProvider(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, &core.Config{
		DefaultLanguage: "en",
		DefaultProvider: "gemini",
	})

	if m.language != "en" {
		t.Errorf("language = %q, want en", m.language)
	}
	if m.provider != "gemini" {
		t.Errorf("provider = %q, want gemini", m.provider)
	}
}

func TestNewModelInitializesTextInput(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)

	if !m.input.Focused() {
		t.Error("input should be focused")
	}
	if m.input.Placeholder != "owner/repo または GitHub URL" {
		t.Errorf("placeholder = %q", m.input.Placeholder)
	}
	if m.input.CharLimit != 300 {
		t.Errorf("char limit = %d, want 300", m.input.CharLimit)
	}
}

func TestNewModelInitializesDotSpinner(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)

	if !reflect.DeepEqual(m.spinner.Spinner, spinner.Dot) {
		t.Errorf("spinner = %#v, want Dot", m.spinner.Spinner)
	}
}

func TestNewModelInitializesViewState(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)

	if m.viewport.Width != 0 || m.viewport.Height != 0 {
		t.Errorf("viewport size = %dx%d, want 0x0", m.viewport.Width, m.viewport.Height)
	}
	if m.tab != tabHistory {
		t.Errorf("tab = %v, want tabHistory", m.tab)
	}
	if m.selected != 0 {
		t.Errorf("selected = %d, want 0", m.selected)
	}
	if m.current != nil {
		t.Errorf("current = %#v, want nil", m.current)
	}
	if m.errMessage != "" {
		t.Errorf("errMessage = %q, want empty", m.errMessage)
	}
}

func TestNewModelKeepsInjectedDependencies(t *testing.T) {
	store := &fakeStore{}
	github := &fakeGitHub{}
	aiClient := &fakeAI{}
	ai := map[string]clients.AIClient{"claude": aiClient}
	now := func() time.Time { return time.Unix(123, 0) }
	renderer := fakeRenderer{output: "rendered"}

	m := NewModel(Dependencies{
		Store:    store,
		GitHub:   github,
		AI:       ai,
		Now:      now,
		Renderer: renderer,
	}, nil)

	if m.store != store {
		t.Error("injected store was not kept")
	}
	if m.github != github {
		t.Error("injected GitHub client was not kept")
	}
	if m.ai["claude"] != aiClient {
		t.Error("injected AI clients were not kept")
	}
	if got := m.now(); !got.Equal(time.Unix(123, 0)) {
		t.Errorf("now() = %v", got)
	}
	if got, err := m.renderer.Render("source", 80); err != nil || got != "rendered" {
		t.Errorf("renderer.Render() = %q, %v", got, err)
	}
}

func TestNewModelProvidesDefaultNow(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)

	before := time.Now()
	got := m.now()
	after := time.Now()
	if got.Before(before) || got.After(after) {
		t.Errorf("now() = %v, want between %v and %v", got, before, after)
	}
}

func TestNewModelProvidesDefaultRenderer(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)

	if _, ok := m.renderer.(glamourRenderer); !ok {
		t.Errorf("renderer = %T, want glamourRenderer", m.renderer)
	}
}

func TestNewModelAllowsNilStore(t *testing.T) {
	m := NewModel(Dependencies{}, nil)

	if m.entries != nil {
		t.Errorf("entries = %#v, want nil", m.entries)
	}
	if len(m.visible) != 0 {
		t.Errorf("visible = %#v, want empty", m.visible)
	}
	if m.errMessage != "" {
		t.Errorf("errMessage = %q, want empty", m.errMessage)
	}
}

func TestNewModelTreatsNilAndEmptyLoadResultsAsEmptyHistory(t *testing.T) {
	for _, tt := range []struct {
		name    string
		entries []*core.Entry
	}{
		{name: "nil"},
		{name: "empty", entries: []*core.Entry{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel(Dependencies{Store: &fakeStore{entries: tt.entries}}, nil)

			if len(m.entries) != 0 || len(m.visible) != 0 {
				t.Errorf("entries = %#v, visible = %#v; want empty", m.entries, m.visible)
			}
		})
	}
}

func TestNewModelKeepsStoreOrderForEqualViewedAt(t *testing.T) {
	viewedAt := time.Unix(10, 0)
	first := &core.Entry{FullName: "first/repo", ViewedAt: viewedAt}
	second := &core.Entry{FullName: "second/repo", ViewedAt: viewedAt}
	m := NewModel(Dependencies{Store: &fakeStore{entries: []*core.Entry{first, second}}}, nil)

	if len(m.entries) != 2 || m.entries[0] != first || m.entries[1] != second {
		t.Fatalf("entries = %#v, want original order", m.entries)
	}
}

func TestNewModelDoesNotModifyStoreSlice(t *testing.T) {
	old := &core.Entry{FullName: "old/repo", ViewedAt: time.Unix(1, 0)}
	newest := &core.Entry{FullName: "new/repo", ViewedAt: time.Unix(2, 0)}
	entries := []*core.Entry{old, nil, newest}
	store := &fakeStore{entries: entries}

	_ = NewModel(Dependencies{Store: store}, nil)

	if len(store.entries) != 3 || store.entries[0] != old || store.entries[1] != nil || store.entries[2] != newest {
		t.Fatalf("store entries were modified: %#v", store.entries)
	}
}

func TestReloadEntriesClearsLoadErrorAfterRecovery(t *testing.T) {
	store := &fakeStore{loadErr: errors.New("broken")}
	m := NewModel(Dependencies{Store: store}, nil)
	if m.errMessage == "" {
		t.Fatal("initial load should set an error message")
	}

	store.loadErr = nil
	store.entries = []*core.Entry{{FullName: "owner/repo"}}
	m.reloadEntries()

	if m.errMessage != "" {
		t.Errorf("errMessage = %q, want empty after successful reload", m.errMessage)
	}
}

func TestReloadEntriesReplacesHistoryAndClampsSelection(t *testing.T) {
	store := &fakeStore{entries: []*core.Entry{
		{FullName: "one/repo"},
		{FullName: "two/repo"},
		{FullName: "three/repo"},
	}}
	m := NewModel(Dependencies{Store: store}, nil)
	m.selected = 2
	latest := &core.Entry{FullName: "latest/repo"}
	store.entries = []*core.Entry{latest}

	m.reloadEntries()

	if len(m.entries) != 1 || m.entries[0] != latest {
		t.Fatalf("entries = %#v, want latest entry", m.entries)
	}
	if len(m.visible) != 1 || m.visible[0] != latest {
		t.Fatalf("visible = %#v, want latest entry", m.visible)
	}
	if m.selected != 0 {
		t.Errorf("selected = %d, want 0", m.selected)
	}
}

func TestRefreshVisibleShowsAllEntriesOnHistoryTab(t *testing.T) {
	first := &core.Entry{FullName: "first/repo"}
	second := &core.Entry{FullName: "second/repo", IsFavorite: true}
	m := NewModel(Dependencies{Store: &fakeStore{entries: []*core.Entry{first, second}}}, nil)

	m.tab = tabHistory
	m.refreshVisible()

	if len(m.visible) != 2 || m.visible[0] != first || m.visible[1] != second {
		t.Fatalf("visible = %#v, want all history entries", m.visible)
	}
}

func TestRefreshVisibleResetsSelectionWhenFavoritesAreEmpty(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{entries: []*core.Entry{{FullName: "owner/repo"}}}}, nil)
	m.tab = tabFavorites
	m.selected = 1

	m.refreshVisible()

	if len(m.visible) != 0 {
		t.Fatalf("visible = %#v, want empty", m.visible)
	}
	if m.selected != 0 {
		t.Errorf("selected = %d, want 0", m.selected)
	}
}

func TestRefreshVisibleClampsNegativeSelection(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{entries: []*core.Entry{{FullName: "owner/repo"}}}}, nil)
	m.selected = -1

	m.refreshVisible()

	if m.selected != 0 {
		t.Errorf("selected = %d, want 0", m.selected)
	}
}

func TestInitStartsTextInputBlinkAndSpinnerTick(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)

	msg := m.Init()()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init message = %T, want tea.BatchMsg", msg)
	}
	if len(batch) != 2 {
		t.Fatalf("batch length = %d, want 2", len(batch))
	}
	if blinkMsg := batch[0](); blinkMsg == nil {
		t.Error("text input blink command returned nil")
	}
	if _, ok := batch[1]().(spinner.TickMsg); !ok {
		t.Errorf("spinner command message = %T, want spinner.TickMsg", batch[1]())
	}
}
