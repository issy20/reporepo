package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/issy20/reporepo/internal/core"
)

type analysisSucceededMsg struct {
	requestID uint64
	entry     *core.Entry
}
type analysisFailedMsg struct {
	requestID uint64
	err       error
}

type entryMutationKind uint8

const (
	mutationFavorite entryMutationKind = iota
	mutationDelete
)

type entryMutationFinishedMsg struct {
	requestID uint64
	kind      entryMutationKind
	fullName  string
	err       error
}

func (m Model) analyzeCmd(ctx context.Context, input string, force bool, requestID uint64) tea.Cmd {
	return func() tea.Msg {
		entry, err := m.analyze(ctx, input, force)
		if err != nil {
			return analysisFailedMsg{requestID: requestID, err: err}
		}
		return analysisSucceededMsg{requestID: requestID, entry: entry}
	}
}

func (m Model) analyze(ctx context.Context, input string, force bool) (*core.Entry, error) {
	return m.analyzer.Analyze(ctx, input, m.language, m.provider, force)
}
