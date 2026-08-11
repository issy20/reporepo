package tui

import (
	"context"
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/issy20/reporepo/internal/analyzer"
	"github.com/issy20/reporepo/internal/core"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		layout := newLayout(msg.Width, msg.Height)
		m.width, m.height = layout.width, layout.height
		m.input.Width = layout.inputWidth
		m.viewport.Width = layout.viewportWidth
		m.viewport.Height = layout.viewportHeight
		m.noteEditor.SetWidth(layout.viewportWidth)
		m.noteEditor.SetHeight(max(3, layout.height-8))
		if m.state == stateDetail {
			m.resizeDetailContent()
		}
		return m, nil
	case analysisSucceededMsg:
		if msg.requestID != m.requestID {
			return m, nil
		}
		m.cancel = nil
		m.requestID++
		if msg.entry == nil {
			m.state = stateInput
			m.errMessage = "解析結果を受け取れませんでした"
			return m, nil
		}
		m.current = msg.entry
		m.state = stateDetail
		m.errMessage = ""
		m.warnings = msg.warnings
		m.reloadEntries()
		m.setDetailContent()
		return m, nil
	case analysisFailedMsg:
		if msg.requestID != m.requestID {
			return m, nil
		}
		m.cancel = nil
		m.requestID++
		m.state = stateInput
		m.warnings = nil
		if msg.err == nil {
			m.errMessage = "解析に失敗しました"
		} else {
			m.errMessage = msg.err.Error()
		}
		return m, nil
	case entryMutationFinishedMsg:
		if msg.requestID != m.mutationRequestID || !m.mutationPending {
			return m, nil
		}
		m.mutationPending = false
		if msg.err != nil {
			m.errMessage = msg.err.Error()
			return m, nil
		}
		m.reloadEntries()
		if m.current != nil {
			m.current = analyzer.FindEntry(m.entries, m.current.FullName)
			if m.state == stateDetail {
				m.setDetailContent()
			}
		}
		return m, nil
	case trendingLoadedMsg:
		if msg.requestID != m.trendingRequestID {
			return m, nil
		}
		m.trendingLoading = false
		m.trendingRepos = msg.repos
		m.trendingSelected = 0
		m.trendingErr = ""
		m.trendingStale = msg.stale
		if msg.stale {
			m.trendingErr = "レート制限のため、キャッシュ済みの一覧を表示しています。時間をおいて再実行してください"
		}
		return m, nil
	case trendingFailedMsg:
		if msg.requestID != m.trendingRequestID {
			return m, nil
		}
		m.trendingLoading = false
		m.trendingErr = msg.err.Error()
		m.trendingStale = false
		return m, nil
	case spinnerTickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg.msg)
		return m, cmd
	case tea.KeyMsg:
		switch m.state {
		case stateLoading:
			return m.updateLoading(msg)
		case stateDetail:
			return m.updateDetail(msg)
		case stateTrending:
			return m.updateTrending(msg)
		default:
			return m.updateInput(msg)
		}
	}
	if m.state == stateLoading || m.trendingLoading {
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

type spinnerTickMsg struct{ msg tea.Msg }

func (m Model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "esc" {
		return m, tea.Quit
	}
	if key == "enter" {
		value := strings.TrimSpace(m.input.Value())
		if value == "" {
			if m.selected < 0 || m.selected >= len(m.visible) || m.visible[m.selected] == nil {
				return m, nil
			}
			value = m.visible[m.selected].FullName
		}
		return m.startAnalysis(value, false)
	}
	if key == "up" {
		if m.selected > 0 {
			m.selected--
		}
		return m, nil
	}
	if key == "down" {
		if m.selected+1 < len(m.visible) {
			m.selected++
		}
		return m, nil
	}
	// 入力中は通常文字を textinput に渡し、リポジトリ名を欠損させない。
	if m.input.Value() == "" {
		switch key {
		case "tab":
			m.tab = (m.tab + 1) % 2
			m.selected = 0
			m.refreshVisible()
			return m, nil
		case "l":
			m.language = toggle(m.language, "ja", "en")
			return m, nil
		case "p":
			m.provider = nextProvider(m.provider, availableProviders(m.ai))
			return m, nil
		case "f":
			return m.toggleFavorite()
		case "d":
			return m.deleteSelected()
		case "t":
			return m.startTrending()
		case "q":
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) updateLoading(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		if m.cancel != nil {
			m.cancel()
		}
		m.cancel = nil
		m.requestID++
		m.state = stateInput
		m.errMessage = ""
	}
	return m, nil
}

func (m Model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.noteEditing {
		return m.updateNoteEditing(msg)
	}
	switch msg.String() {
	case "esc":
		m.state = stateInput
		m.current = nil
		return m, nil
	case "n":
		if m.current == nil {
			return m, nil
		}
		m.noteEditor.SetValue(m.current.Note)
		m.noteEditing = true
		return m, m.noteEditor.Focus()
	case "f":
		return m.toggleFavorite()
	case "r":
		if m.current != nil {
			return m.startAnalysis(m.current.FullName, true)
		}
		return m, nil
	case "l":
		m.language = toggle(m.language, "ja", "en")
		if m.current == nil {
			return m, nil
		}
		if m.current.Analyses[m.language] != nil {
			m.setDetailContent()
			return m, nil
		}
		return m.startAnalysis(m.current.FullName, false)
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// updateNoteEditing はノート編集モード中のキー操作を処理する。
func (m Model) updateNoteEditing(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+s":
		if m.mutationPending || m.current == nil || m.store == nil {
			return m, nil
		}
		m.noteEditing = false
		m.noteEditor.Blur()
		m.mutationRequestID++
		m.mutationPending = true
		return m, m.saveNoteCmd(m.noteEditor.Value())
	case "esc":
		m.noteEditing = false
		m.noteEditor.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.noteEditor, cmd = m.noteEditor.Update(msg)
	return m, cmd
}

func (m Model) startAnalysis(input string, force bool) (tea.Model, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.requestID++
	m.state = stateLoading
	m.errMessage = ""
	m.loadingLabel = "解析しています: " + input
	return m, m.analyzeCmd(ctx, input, force, m.requestID)
}

// startTrending は Trending 一覧画面へ遷移し、非同期で一覧取得を開始する。
func (m Model) startTrending() (tea.Model, tea.Cmd) {
	if m.github == nil {
		m.errMessage = errTrendingFetch.Error()
		return m, nil
	}
	m.state = stateTrending
	m.trendingRepos = nil
	m.trendingSelected = 0
	m.trendingErr = ""
	m.trendingStale = false
	m.trendingLoading = true
	m.trendingRequestID++
	requestID := m.trendingRequestID
	return m, m.trendingCmd(requestID)
}

// updateTrending は Trending 一覧画面のキー操作を処理する。
func (m Model) updateTrending(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.trendingRequestID++
		m.trendingLoading = false
		m.state = stateInput
		return m, nil
	case "enter":
		if m.trendingLoading || m.trendingSelected < 0 || m.trendingSelected >= len(m.trendingRepos) || m.trendingRepos[m.trendingSelected].FullName == "" {
			return m, nil
		}
		return m.startAnalysis(m.trendingRepos[m.trendingSelected].FullName, false)
	case "up":
		if m.trendingSelected > 0 {
			m.trendingSelected--
		}
		return m, nil
	case "down":
		if m.trendingSelected+1 < len(m.trendingRepos) {
			m.trendingSelected++
		}
		return m, nil
	case "t":
		return m.startTrending()
	}
	return m, nil
}

func (m Model) toggleFavorite() (tea.Model, tea.Cmd) {
	if m.mutationPending {
		return m, nil
	}
	entry := m.current
	if m.state == stateInput {
		if m.selected < 0 || m.selected >= len(m.visible) {
			return m, nil
		}
		entry = m.visible[m.selected]
	}
	if entry == nil || m.store == nil {
		return m, nil
	}
	updated := analyzer.CloneEntry(entry)
	updated.IsFavorite = !updated.IsFavorite
	m.mutationRequestID++
	m.mutationPending = true
	requestID := m.mutationRequestID
	fullName := entry.FullName
	store := m.store
	return m, func() tea.Msg {
		err := store.Upsert(updated)
		return entryMutationFinishedMsg{requestID: requestID, kind: mutationFavorite, fullName: fullName, err: userStoreError(err)}
	}
}

func (m Model) deleteSelected() (tea.Model, tea.Cmd) {
	if m.mutationPending || m.selected < 0 || m.selected >= len(m.visible) || m.store == nil {
		return m, nil
	}
	target := m.visible[m.selected]
	if target == nil {
		return m, nil
	}
	entries := make([]*core.Entry, 0, len(m.entries)-1)
	for _, entry := range m.entries {
		if entry != nil && !strings.EqualFold(entry.FullName, target.FullName) {
			entries = append(entries, entry)
		}
	}
	m.mutationRequestID++
	m.mutationPending = true
	requestID := m.mutationRequestID
	fullName := target.FullName
	store := m.store
	return m, func() tea.Msg {
		return entryMutationFinishedMsg{requestID: requestID, kind: mutationDelete, fullName: fullName, err: userStoreError(store.Save(entries))}
	}
}

func userStoreError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New("履歴を保存できませんでした")
}
func toggle(value, a, b string) string {
	if value == a {
		return b
	}
	return a
}
