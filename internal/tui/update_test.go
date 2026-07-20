package tui

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/issy20/reporepo/internal/core"
)

func updated(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	got, ok := next.(Model)
	if !ok {
		t.Fatalf("model type %T", next)
	}
	return got, cmd
}

func TestEnterStartsAnalysisAndSuccessOpensDetail(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	m.input.SetValue("owner/repo")
	loading, cmd := updated(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if loading.state != stateLoading || cmd == nil || loading.requestID == 0 {
		t.Fatalf("state=%v cmd=%v id=%d", loading.state, cmd, loading.requestID)
	}
	entry := &core.Entry{FullName: "owner/repo", Analyses: map[string]*core.Analysis{"ja": {Summary: "summary"}}}
	detail, _ := updated(t, loading, analysisSucceededMsg{requestID: loading.requestID, entry: entry})
	if detail.state != stateDetail || detail.current != entry {
		t.Fatalf("state=%v current=%#v", detail.state, detail.current)
	}
}

func TestFailureReturnsToInputAndOldResultIsIgnored(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	m.state = stateLoading
	m.requestID = 2
	old, _ := updated(t, m, analysisSucceededMsg{requestID: 1, entry: &core.Entry{FullName: "old/repo"}})
	if old.state != stateLoading || old.current != nil {
		t.Fatal("old result applied")
	}
	failed, _ := updated(t, m, analysisFailedMsg{requestID: 2, err: errors.New("failed")})
	if failed.state != stateInput || failed.errMessage != "failed" {
		t.Fatalf("state=%v err=%q", failed.state, failed.errMessage)
	}
}

func TestLoadingEscapeCancelsAndInvalidatesRequest(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	next, _ := m.startAnalysis("owner/repo", false)
	loading := next.(Model)
	id := loading.requestID
	cancelled, _ := updated(t, loading, tea.KeyMsg{Type: tea.KeyEsc})
	if cancelled.state != stateInput || cancelled.cancel != nil || cancelled.requestID == id {
		t.Fatalf("state=%v cancel=%v id=%d", cancelled.state, cancelled.cancel, cancelled.requestID)
	}
}

func TestInputNavigationTogglesAndTypingShortcuts(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{entries: []*core.Entry{{FullName: "a/a"}, {FullName: "b/b", IsFavorite: true}}}}, nil)
	m, _ = updated(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selected != 1 {
		t.Fatalf("selected=%d", m.selected)
	}
	m, _ = updated(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.tab != tabFavorites || len(m.visible) != 1 {
		t.Fatalf("tab=%v visible=%d", m.tab, len(m.visible))
	}
	m, _ = updated(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if m.language != "en" {
		t.Fatalf("language=%s", m.language)
	}
	m.input.SetValue("owner/")
	m, _ = updated(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if m.input.Value() != "owner/p" || m.provider != "claude" {
		t.Fatalf("input=%q provider=%s", m.input.Value(), m.provider)
	}
}

func TestDetailLanguageUsesCacheOrStartsAnalysis(t *testing.T) {
	entry := &core.Entry{FullName: "owner/repo", Analyses: map[string]*core.Analysis{"ja": {}, "en": {Summary: "cached"}}}
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	m.state = stateDetail
	m.current = entry
	m.setDetailContent()
	cached, cmd := updated(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if cached.state != stateDetail || cached.language != "en" || cmd != nil {
		t.Fatalf("state=%v lang=%s cmd=%v", cached.state, cached.language, cmd)
	}
	delete(entry.Analyses, "ja")
	missing, cmd := updated(t, cached, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if missing.state != stateLoading || missing.language != "ja" || cmd == nil {
		t.Fatalf("state=%v lang=%s cmd=%v", missing.state, missing.language, cmd)
	}
}

func TestWindowSizeUpdatesComponents(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	got, _ := updated(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if got.input.Width != 76 || got.viewport.Width != 78 || got.viewport.Height != 21 {
		t.Fatalf("input=%d viewport=%dx%d", got.input.Width, got.viewport.Width, got.viewport.Height)
	}
}

func TestFavoriteAndDeletePersistSelection(t *testing.T) {
	entry := &core.Entry{FullName: "owner/repo"}
	store := &recordingStore{entries: []*core.Entry{entry}}
	m := NewModel(Dependencies{Store: store}, nil)
	next, cmd := updated(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if cmd == nil || !entry.IsFavorite {
		t.Fatal("favorite was not toggled")
	}
	msg := cmd()
	next, _ = updated(t, next, msg)
	if store.upsertCalls != 1 || !entry.IsFavorite {
		t.Fatalf("upsert=%d favorite=%v", store.upsertCalls, entry.IsFavorite)
	}
	// お気に入り切替コマンドの再読込後も履歴タブで削除できる。
	next.input.SetValue("")
	deleted, deleteCmd := updated(t, next, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if deleteCmd == nil {
		t.Fatal("delete command is nil")
	}
	deleteMsg := deleteCmd()
	deleted, _ = updated(t, deleted, deleteMsg)
	if store.saveCalls != 1 || len(deleted.entries) != 0 {
		t.Fatalf("save=%d entries=%d", store.saveCalls, len(deleted.entries))
	}
}

func TestEmptyListFavoriteAndDeleteAreNoops(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	for _, key := range []rune{'f', 'd'} {
		got, cmd := updated(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		if cmd != nil || len(got.entries) != 0 {
			t.Fatalf("key=%c cmd=%v", key, cmd)
		}
	}
}
