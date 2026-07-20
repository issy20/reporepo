package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/issy20/reporepo/internal/core"
)

// NewProgram は注入された依存関係から Bubble Tea program を構築する。
func NewProgram(deps Dependencies, cfg *core.Config, options ...tea.ProgramOption) *tea.Program {
	m := NewModel(deps, cfg)
	return tea.NewProgram(m, options...)
}

// Run は TUI を起動し、終了まで待機する。
func Run(deps Dependencies, cfg *core.Config, options ...tea.ProgramOption) error {
	_, err := NewProgram(deps, cfg, options...).Run()
	return err
}
