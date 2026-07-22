package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
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

func TestStartAnalysisClearsErrorSetsLabelAndUsesSelectedHistory(t *testing.T) {
	entry := &core.Entry{FullName: "selected/repo"}
	m := NewModel(Dependencies{Store: &fakeStore{entries: []*core.Entry{entry}}}, nil)
	m.errMessage = "old error"
	loading, cmd := updated(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if loading.state != stateLoading || loading.cancel == nil || loading.errMessage != "" || loading.loadingLabel != "解析しています: selected/repo" || cmd == nil {
		t.Fatalf("state=%v cancel=%v err=%q label=%q cmd=%v", loading.state, loading.cancel, loading.errMessage, loading.loadingLabel, cmd)
	}
}

func TestEmptyInputAndHistoryDoesNotStartAnalysis(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	got, cmd := updated(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if got.state != stateInput || got.requestID != 0 || got.cancel != nil || cmd != nil {
		t.Fatalf("state=%v id=%d cancel=%v cmd=%v", got.state, got.requestID, got.cancel, cmd)
	}
}

func TestLoadingKeysDoNotStartSecondAnalysis(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	next, _ := m.startAnalysis("owner/repo", false)
	loading := next.(Model)
	id := loading.requestID
	got, cmd := updated(t, loading, tea.KeyMsg{Type: tea.KeyEnter})
	if got.requestID != id || got.cancel == nil || cmd != nil {
		t.Fatalf("id=%d cancel=%v cmd=%v", got.requestID, got.cancel, cmd)
	}
}

func TestSuccessReloadsHistoryUpdatesDetailAndIsAppliedOnce(t *testing.T) {
	entry := &core.Entry{FullName: "owner/repo", Analyses: map[string]*core.Analysis{"ja": {Summary: "summary"}}}
	store := &fakeStore{entries: []*core.Entry{entry}}
	m := NewModel(Dependencies{Store: store, Renderer: fakeRenderer{output: "rendered detail"}}, nil)
	m.state, m.requestID, m.errMessage = stateLoading, 7, "old"
	m.viewport.Width, m.viewport.Height = 80, 20
	m.cancel = func() {}
	got, _ := updated(t, m, analysisSucceededMsg{requestID: 7, entry: entry})
	if got.state != stateDetail || got.current != entry || got.cancel != nil || got.errMessage != "" || len(got.entries) != 1 || !strings.Contains(got.viewport.View(), "rendered detail") {
		t.Fatalf("state=%v current=%#v cancel=%v err=%q entries=%d detail=%q", got.state, got.current, got.cancel, got.errMessage, len(got.entries), got.viewport.View())
	}
	duplicate := &core.Entry{FullName: "duplicate/repo"}
	again, _ := updated(t, got, analysisSucceededMsg{requestID: 7, entry: duplicate})
	if again.current != entry || again.state != stateDetail {
		t.Fatal("same result was applied twice")
	}
}

func TestNilAnalysisMessagesFailSafelyWithoutChangingData(t *testing.T) {
	current := &core.Entry{FullName: "current/repo"}
	entries := []*core.Entry{current}
	for _, msg := range []tea.Msg{
		analysisSucceededMsg{requestID: 3, entry: nil},
		analysisFailedMsg{requestID: 3, err: nil},
	} {
		m := NewModel(Dependencies{Store: &fakeStore{entries: entries}}, nil)
		m.state, m.requestID, m.current = stateLoading, 3, current
		got, _ := updated(t, m, msg)
		if got.state != stateInput || got.current != current || len(got.entries) != 1 || got.errMessage == "" || got.cancel != nil {
			t.Fatalf("msg=%T state=%v current=%#v entries=%d err=%q", msg, got.state, got.current, len(got.entries), got.errMessage)
		}
	}
}

func TestOldFailureAndPostCancellationResultsAreIgnored(t *testing.T) {
	current := &core.Entry{FullName: "current/repo"}
	m := NewModel(Dependencies{Store: &fakeStore{entries: []*core.Entry{current}}}, nil)
	m.state, m.requestID, m.current, m.errMessage = stateLoading, 10, current, "keep"
	oldFailure, _ := updated(t, m, analysisFailedMsg{requestID: 9, err: errors.New("old")})
	if oldFailure.state != stateLoading || oldFailure.errMessage != "keep" || oldFailure.current != current {
		t.Fatal("old failure applied")
	}
	cancelled, _ := updated(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if cancelled.state != stateInput || cancelled.errMessage != "" {
		t.Fatalf("state=%v err=%q", cancelled.state, cancelled.errMessage)
	}
	for _, msg := range []tea.Msg{
		analysisSucceededMsg{requestID: 10, entry: &core.Entry{FullName: "late/repo"}},
		analysisFailedMsg{requestID: 10, err: errors.New("late")},
	} {
		got, _ := updated(t, cancelled, msg)
		if got.state != stateInput || got.current != current || len(got.entries) != 1 || got.errMessage != "" {
			t.Fatalf("late %T applied", msg)
		}
	}
}

func TestLoadingSpinnerTickContinues(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	m.state = stateLoading
	_, cmd := updated(t, m, spinnerTickMsg{msg: spinner.TickMsg{}})
	if cmd == nil {
		t.Fatal("spinner command is nil")
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

func TestWindowSizeZeroKeepsAllComponentSizesPositive(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	got, _ := updated(t, m, tea.WindowSizeMsg{})
	if got.width < 1 || got.height < 1 || got.input.Width < 1 || got.viewport.Width < 1 || got.viewport.Height < 1 {
		t.Fatalf("model=%dx%d input=%d viewport=%dx%d", got.width, got.height, got.input.Width, got.viewport.Width, got.viewport.Height)
	}
}

func TestConsecutiveWindowSizesStayConsistentAndRerenderAtNewWidth(t *testing.T) {
	renderer := &recordingRenderer{}
	m := NewModel(Dependencies{Store: &fakeStore{}, Renderer: renderer}, nil)
	m.state, m.current = stateDetail, &core.Entry{FullName: "owner/repo"}
	for _, size := range []tea.WindowSizeMsg{{Width: 100, Height: 30}, {Width: 12, Height: 4}, {Width: 0, Height: 0}} {
		m, _ = updated(t, m, size)
		layout := newLayout(size.Width, size.Height)
		if m.width != layout.width || m.height != layout.height || m.input.Width != layout.inputWidth || m.viewport.Width != layout.viewportWidth || m.viewport.Height != layout.viewportHeight || renderer.width != max(1, layout.width-4) {
			t.Fatalf("size=%#v model=%dx%d input=%d viewport=%dx%d renderWidth=%d", size, m.width, m.height, m.input.Width, m.viewport.Width, m.viewport.Height, renderer.width)
		}
	}
}

func TestDetailContentChangesResetViewportToTop(t *testing.T) {
	renderer := &resizableRenderer{lines: 100}
	m := NewModel(Dependencies{Store: &fakeStore{}, Renderer: renderer}, nil)
	m.state = stateDetail
	m.current = &core.Entry{FullName: "owner/repo", Analyses: map[string]*core.Analysis{"ja": {}, "en": {}}}
	m.viewport.Width, m.viewport.Height = 78, 10
	m.setDetailContent()
	m.viewport.SetYOffset(40)
	m, _ = updated(t, m, runeKey('l'))
	if m.viewport.YOffset != 0 {
		t.Fatalf("language change offset=%d", m.viewport.YOffset)
	}
	m.viewport.SetYOffset(40)
	m.requestID = 7
	m, _ = updated(t, m, analysisSucceededMsg{requestID: 7, entry: &core.Entry{FullName: "new/repo"}})
	if m.viewport.YOffset != 0 {
		t.Fatalf("entry change offset=%d", m.viewport.YOffset)
	}
}

func TestDetailResizePreservesScrollPosition(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "line"
	}
	m := NewModel(Dependencies{Store: &fakeStore{}, Renderer: fakeRenderer{output: strings.Join(lines, "\n")}}, nil)
	m.state = stateDetail
	m.current = &core.Entry{FullName: "owner/repo"}
	m.viewport.Width, m.viewport.Height = 78, 10
	m.setDetailContent()
	m.viewport.SetYOffset(45)

	got, _ := updated(t, m, tea.WindowSizeMsg{Width: 60, Height: 13})
	if got.viewport.YOffset == 0 {
		t.Fatalf("scroll position reset on resize: offset=%d", got.viewport.YOffset)
	}
}

func TestDetailResizeClampsOffsetWhenContentBecomesShort(t *testing.T) {
	renderer := &resizableRenderer{lines: 100}
	m := NewModel(Dependencies{Store: &fakeStore{}, Renderer: renderer}, nil)
	m.state = stateDetail
	m.current = &core.Entry{FullName: "owner/repo"}
	m.viewport.Width, m.viewport.Height = 78, 10
	m.setDetailContent()
	m.viewport.GotoBottom()
	renderer.lines = 2

	got, _ := updated(t, m, tea.WindowSizeMsg{Width: 60, Height: 20})
	if got.viewport.YOffset != 0 {
		t.Fatalf("offset=%d, want 0 for short content", got.viewport.YOffset)
	}
}

type resizableRenderer struct{ lines int }

func (r *resizableRenderer) Render(string, int) (string, error) {
	return strings.Repeat("line\n", r.lines), nil
}

func TestFavoriteUsesCopyWithoutChangingOriginalBeforeResult(t *testing.T) {
	entry := &core.Entry{FullName: "owner/repo"}
	store := &recordingStore{entries: []*core.Entry{entry}}
	m := NewModel(Dependencies{Store: store}, nil)
	next, cmd := updated(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if cmd == nil {
		t.Fatal("favorite command is nil")
	}
	if entry.IsFavorite || next.entries[0].IsFavorite || next.visible[0].IsFavorite {
		t.Fatal("original entry changed before mutation result")
	}
	_ = cmd()
	if entry.IsFavorite || next.entries[0].IsFavorite || next.visible[0].IsFavorite {
		t.Fatal("original entry changed by mutation command")
	}
	if store.upsertCalls != 1 || len(store.entries) != 1 || !store.entries[0].IsFavorite || store.entries[0] == entry {
		t.Fatalf("upsert=%d stored=%#v", store.upsertCalls, store.entries)
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

func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func TestFavoriteMutationSuccessReloadsAndSynchronizesCurrent(t *testing.T) {
	entry := &core.Entry{FullName: "owner/repo", Analyses: map[string]*core.Analysis{"ja": {}}}
	store := &recordingStore{entries: []*core.Entry{entry}}
	m := NewModel(Dependencies{Store: store}, nil)
	m.state, m.current = stateDetail, entry

	pending, cmd := updated(t, m, runeKey('f'))
	if !pending.mutationPending || pending.mutationRequestID != 1 || cmd == nil {
		t.Fatalf("pending=%v id=%d cmd=%v", pending.mutationPending, pending.mutationRequestID, cmd)
	}
	msg := cmd()
	got, _ := updated(t, pending, msg)
	if got.mutationPending || got.current == nil || !got.current.IsFavorite || !got.entries[0].IsFavorite || got.current == entry {
		t.Fatalf("pending=%v current=%#v entries=%#v", got.mutationPending, got.current, got.entries)
	}
}

func TestFavoriteFailureKeepsStateAndSanitizesError(t *testing.T) {
	entry := &core.Entry{FullName: "owner/repo"}
	store := &recordingStore{entries: []*core.Entry{entry}, upsertErr: errors.New("secret path /tmp/history.json")}
	m := NewModel(Dependencies{Store: store}, nil)
	loadsBefore := store.loadCalls
	pending, cmd := updated(t, m, runeKey('f'))
	got, _ := updated(t, pending, cmd())
	if got.mutationPending || got.entries[0] != entry || got.visible[0] != entry || entry.IsFavorite {
		t.Fatalf("state changed on failure: %#v", got.entries)
	}
	if store.loadCalls != loadsBefore || got.errMessage != "履歴を保存できませんでした" || strings.Contains(got.errMessage, "secret") {
		t.Fatalf("loads=%d err=%q", store.loadCalls, got.errMessage)
	}
}

func TestMutationPendingSuppressesFavoriteAndDeleteAndOldResult(t *testing.T) {
	entry := &core.Entry{FullName: "owner/repo"}
	store := &recordingStore{entries: []*core.Entry{entry}}
	m := NewModel(Dependencies{Store: store}, nil)
	pending, cmd := updated(t, m, runeKey('f'))
	for _, key := range []rune{'f', 'd'} {
		got, blockedCmd := updated(t, pending, runeKey(key))
		if blockedCmd != nil || got.mutationRequestID != pending.mutationRequestID {
			t.Fatalf("key=%c was not suppressed", key)
		}
	}
	old, _ := updated(t, pending, entryMutationFinishedMsg{requestID: pending.mutationRequestID - 1, kind: mutationFavorite, err: errors.New("old")})
	if !old.mutationPending || old.errMessage != "" || old.entries[0] != entry {
		t.Fatal("old mutation result was applied")
	}
	got, _ := updated(t, pending, cmd())
	if got.mutationPending {
		t.Fatal("current mutation result did not clear pending")
	}
}

func TestFavoriteSuccessInFavoritesTabRemovesEntryAndClampsSelection(t *testing.T) {
	first := &core.Entry{FullName: "first/repo", IsFavorite: true}
	second := &core.Entry{FullName: "second/repo", IsFavorite: true}
	store := &recordingStore{entries: []*core.Entry{first, second}}
	m := NewModel(Dependencies{Store: store}, nil)
	m.tab, m.selected = tabFavorites, 1
	m.refreshVisible()
	pending, cmd := updated(t, m, runeKey('f'))
	got, _ := updated(t, pending, cmd())
	if len(got.visible) != 1 || got.visible[0].FullName != "first/repo" || got.selected != 0 {
		t.Fatalf("visible=%#v selected=%d", got.visible, got.selected)
	}
}

func TestDeleteUsesCaseInsensitiveFullNameAndDoesNotMutateBeforeResult(t *testing.T) {
	targetInEntries := &core.Entry{FullName: "Owner/Repo"}
	keep := &core.Entry{FullName: "keep/repo"}
	store := &recordingStore{entries: []*core.Entry{targetInEntries, keep}}
	m := NewModel(Dependencies{Store: store}, nil)
	m.visible = []*core.Entry{{FullName: "owner/repo"}}
	originalEntries := append([]*core.Entry(nil), m.entries...)
	pending, cmd := updated(t, m, runeKey('d'))
	if cmd == nil || len(pending.entries) != 2 || pending.entries[0] != originalEntries[0] || pending.entries[1] != originalEntries[1] {
		t.Fatal("model entries changed before delete result")
	}
	msg := cmd()
	if store.saveCalls != 1 || len(store.entries) != 1 || store.entries[0] != keep {
		t.Fatalf("saved=%#v calls=%d", store.entries, store.saveCalls)
	}
	got, _ := updated(t, pending, msg)
	if len(got.entries) != 1 || got.entries[0] != keep {
		t.Fatalf("entries=%#v", got.entries)
	}
}

func TestDeleteFailureKeepsEntriesSelectionAndError(t *testing.T) {
	first := &core.Entry{FullName: "first/repo"}
	second := &core.Entry{FullName: "second/repo"}
	store := &recordingStore{entries: []*core.Entry{first, second}, saveErr: errors.New("raw database failure")}
	m := NewModel(Dependencies{Store: store}, nil)
	m.selected = 1
	loadsBefore := store.loadCalls
	pending, cmd := updated(t, m, runeKey('d'))
	got, _ := updated(t, pending, cmd())
	if len(got.entries) != 2 || len(got.visible) != 2 || got.selected != 1 || got.entries[0] != first || got.entries[1] != second {
		t.Fatalf("entries=%#v visible=%#v selected=%d", got.entries, got.visible, got.selected)
	}
	if store.loadCalls != loadsBefore || got.errMessage != "履歴を保存できませんでした" {
		t.Fatalf("loads=%d err=%q", store.loadCalls, got.errMessage)
	}
}

func TestDeleteLastEntryResetsSelection(t *testing.T) {
	entry := &core.Entry{FullName: "owner/repo", IsFavorite: true}
	store := &recordingStore{entries: []*core.Entry{entry}}
	m := NewModel(Dependencies{Store: store}, nil)
	m.tab = tabFavorites
	m.refreshVisible()
	pending, cmd := updated(t, m, runeKey('d'))
	got, _ := updated(t, pending, cmd())
	if len(got.entries) != 0 || len(got.visible) != 0 || got.selected != 0 {
		t.Fatalf("entries=%d visible=%d selected=%d", len(got.entries), len(got.visible), got.selected)
	}
}

func TestDeleteTailClampsSelectionToNewTail(t *testing.T) {
	first := &core.Entry{FullName: "first/repo"}
	second := &core.Entry{FullName: "second/repo"}
	store := &recordingStore{entries: []*core.Entry{first, second}}
	m := NewModel(Dependencies{Store: store}, nil)
	m.selected = 1
	pending, cmd := updated(t, m, runeKey('d'))
	got, _ := updated(t, pending, cmd())
	if len(got.visible) != 1 || got.visible[0] != first || got.selected != 0 {
		t.Fatalf("visible=%#v selected=%d", got.visible, got.selected)
	}
}

func TestSuccessfulMutationReportsReloadFailure(t *testing.T) {
	entry := &core.Entry{FullName: "owner/repo"}
	store := &recordingStore{entries: []*core.Entry{entry}}
	m := NewModel(Dependencies{Store: store}, nil)
	pending, cmd := updated(t, m, runeKey('f'))
	msg := cmd()
	store.loadErr = errors.New("load failed")
	got, _ := updated(t, pending, msg)
	if got.mutationPending || got.errMessage != "履歴を読み込めませんでした" || got.entries[0] != entry {
		t.Fatalf("pending=%v err=%q entries=%#v", got.mutationPending, got.errMessage, got.entries)
	}
}

func TestInputNavigationClampsAndTabRoundTrips(t *testing.T) {
	entries := []*core.Entry{{FullName: "a/a", IsFavorite: true}, {FullName: "b/b"}}
	m := NewModel(Dependencies{Store: &fakeStore{entries: entries}}, nil)
	m, _ = updated(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.selected != 0 {
		t.Fatalf("selected after up at start=%d", m.selected)
	}
	m, _ = updated(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m, _ = updated(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selected != 1 {
		t.Fatalf("selected after down at end=%d", m.selected)
	}
	m, _ = updated(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.tab != tabFavorites || m.selected != 0 || len(m.visible) != 1 {
		t.Fatalf("favorites tab=%v selected=%d visible=%d", m.tab, m.selected, len(m.visible))
	}
	m, _ = updated(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.tab != tabHistory || m.selected != 0 || len(m.visible) != 2 {
		t.Fatalf("history tab=%v selected=%d visible=%d", m.tab, m.selected, len(m.visible))
	}
}

func TestEmptyHistoryNavigationDoesNotPanic(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	for _, key := range []tea.KeyType{tea.KeyUp, tea.KeyDown, tea.KeyTab} {
		var cmd tea.Cmd
		m, cmd = updated(t, m, tea.KeyMsg{Type: key})
		if cmd != nil || m.selected != 0 || len(m.visible) != 0 {
			t.Fatalf("key=%v selected=%d visible=%d cmd=%v", key, m.selected, len(m.visible), cmd)
		}
	}
}

func TestEnterTrimsInput(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	m.input.SetValue("  owner/repo  ")
	got, cmd := updated(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || got.state != stateLoading || got.loadingLabel != "解析しています: owner/repo" {
		t.Fatalf("state=%v label=%q cmd=%v", got.state, got.loadingLabel, cmd)
	}
}

func TestEmptyEnterWithOutOfRangeSelectionIsNoop(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{entries: []*core.Entry{{FullName: "owner/repo"}}}}, nil)
	m.selected = 99
	got, cmd := updated(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || got.state != stateInput {
		t.Fatalf("state=%v cmd=%v", got.state, cmd)
	}
}

func TestEmptyInputShortcutsToggleAndQuit(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	for _, tt := range []struct {
		key      rune
		language string
		provider string
	}{
		{key: 'l', language: "en", provider: "claude"},
		{key: 'l', language: "ja", provider: "claude"},
		{key: 'p', language: "ja", provider: "openai"},
		{key: 'p', language: "ja", provider: "claude"},
	} {
		m, _ = updated(t, m, runeKey(tt.key))
		if m.language != tt.language || m.provider != tt.provider {
			t.Fatalf("key=%c language=%s provider=%s", tt.key, m.language, m.provider)
		}
	}
	_, qcmd := updated(t, m, runeKey('q'))
	if qcmd == nil {
		t.Fatal("q did not return quit command")
	}
	m.input.SetValue("typing")
	_, escCmd := updated(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if escCmd == nil {
		t.Fatal("escape did not return quit command")
	}
}

func TestTypingReservedAndRegularRunesGoesToTextInput(t *testing.T) {
	for _, r := range []rune{'l', 'p', 'f', 'd', 'q', 'x'} {
		m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
		m.input.SetValue("owner/")
		got, _ := updated(t, m, runeKey(r))
		if got.input.Value() != "owner/"+string(r) || got.language != "ja" || got.provider != "claude" {
			t.Fatalf("rune=%c input=%q language=%s provider=%s", r, got.input.Value(), got.language, got.provider)
		}
	}
}

func TestTabWhileTypingDoesNotSwitchListTab(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	m.input.SetValue("owner/")
	got, _ := updated(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if got.tab != tabHistory || got.input.Value() != "owner/" {
		t.Fatalf("tab=%v input=%q", got.tab, got.input.Value())
	}
}

func TestNilDependenciesAndDetailCurrentMutationKeysAreNoops(t *testing.T) {
	m := NewModel(Dependencies{}, nil)
	m.entries = []*core.Entry{{FullName: "owner/repo"}}
	m.refreshVisible()
	for _, key := range []rune{'f', 'd'} {
		got, cmd := updated(t, m, runeKey(key))
		if cmd != nil || got.mutationPending {
			t.Fatalf("input key=%c cmd=%v pending=%v", key, cmd, got.mutationPending)
		}
	}
	m.state, m.current = stateDetail, nil
	for _, key := range []rune{'f', 'r'} {
		got, cmd := updated(t, m, runeKey(key))
		if cmd != nil || got.state != stateDetail {
			t.Fatalf("detail key=%c state=%v cmd=%v", key, got.state, cmd)
		}
	}
}

func TestDetailLanguageHandlesNilAnalyses(t *testing.T) {
	entry := &core.Entry{FullName: "owner/repo"}
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	m.state, m.current = stateDetail, entry
	got, cmd := updated(t, m, runeKey('l'))
	if got.language != "en" || got.state != stateLoading || cmd == nil {
		t.Fatalf("language=%s state=%v cmd=%v", got.language, got.state, cmd)
	}
}

func TestDetailEscapeClearsCurrent(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	m.state, m.current = stateDetail, &core.Entry{FullName: "owner/repo"}
	got, cmd := updated(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if got.state != stateInput || got.current != nil || cmd != nil {
		t.Fatalf("state=%v current=%#v cmd=%v", got.state, got.current, cmd)
	}
}

func TestDetailNavigationIsPassedToViewport(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	m.state = stateDetail
	m.viewport.Width, m.viewport.Height = 20, 2
	m.viewport.SetContent("one\ntwo\nthree\nfour\nfive\nsix")
	for _, key := range []tea.KeyType{tea.KeyDown, tea.KeyPgDown, tea.KeyUp, tea.KeyPgUp} {
		before := m.viewport.YOffset
		m, _ = updated(t, m, tea.KeyMsg{Type: key})
		if (key == tea.KeyDown || key == tea.KeyPgDown) && m.viewport.YOffset <= before {
			t.Fatalf("key=%v offset did not increase: %d -> %d", key, before, m.viewport.YOffset)
		}
		if (key == tea.KeyUp || key == tea.KeyPgUp) && m.viewport.YOffset >= before {
			t.Fatalf("key=%v offset did not decrease: %d -> %d", key, before, m.viewport.YOffset)
		}
	}
}
