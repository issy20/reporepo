package tui

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/issy20/reporepo/internal/clients"
	"github.com/issy20/reporepo/internal/core"
)

type screenState uint8

const (
	stateInput screenState = iota
	stateLoading
	stateDetail
)

type listTab uint8

const (
	tabHistory listTab = iota
	tabFavorites
)

type entryStore interface {
	Load() ([]*core.Entry, error)
	Save([]*core.Entry) error
	Upsert(*core.Entry) error
}

// Dependencies は TUI が利用する外部境界をまとめる。
type Dependencies struct {
	Store    entryStore
	GitHub   clients.GitHubClient
	AI       map[string]clients.AIClient
	Now      func() time.Time
	Renderer markdownRenderer
}

type Model struct {
	state         screenState
	input         textinput.Model
	spinner       spinner.Model
	viewport      viewport.Model
	width, height int

	entries      []*core.Entry
	visible      []*core.Entry
	selected     int
	tab          listTab
	current      *core.Entry
	language     string
	provider     string
	errMessage   string
	loadingLabel string

	store     entryStore
	github    clients.GitHubClient
	ai        map[string]clients.AIClient
	now       func() time.Time
	renderer  markdownRenderer
	cancel    context.CancelFunc
	requestID uint64
}

func NewModel(deps Dependencies, cfg *core.Config) Model {
	language, provider := "ja", "claude"
	if cfg != nil {
		if cfg.DefaultLanguage == "ja" || cfg.DefaultLanguage == "en" {
			language = cfg.DefaultLanguage
		}
		if strings.TrimSpace(cfg.DefaultProvider) != "" {
			provider = cfg.DefaultProvider
		}
	}
	input := textinput.New()
	input.Placeholder = "owner/repo または GitHub URL"
	input.Prompt = "> "
	input.CharLimit = 300
	input.Focus()
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	renderer := deps.Renderer
	if renderer == nil {
		renderer = glamourRenderer{}
	}
	m := Model{state: stateInput, input: input, spinner: sp, viewport: viewport.New(0, 0), language: language, provider: provider, store: deps.Store, github: deps.GitHub, ai: deps.AI, now: now, renderer: renderer}
	m.reloadEntries()
	return m
}

func (m *Model) reloadEntries() {
	if m.store == nil {
		m.entries = nil
		m.refreshVisible()
		return
	}
	entries, err := m.store.Load()
	if err != nil {
		m.errMessage = "履歴を読み込めませんでした"
		return
	}
	m.entries = entries[:0]
	for _, entry := range entries {
		if entry != nil {
			m.entries = append(m.entries, entry)
		}
	}
	sort.SliceStable(m.entries, func(i, j int) bool { return m.entries[i].ViewedAt.After(m.entries[j].ViewedAt) })
	m.refreshVisible()
}

func (m *Model) refreshVisible() {
	m.visible = m.visible[:0]
	for _, entry := range m.entries {
		if entry != nil && (m.tab == tabHistory || entry.IsFavorite) {
			m.visible = append(m.visible, entry)
		}
	}
	if len(m.visible) == 0 {
		m.selected = 0
	} else if m.selected >= len(m.visible) {
		m.selected = len(m.visible) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

func (m Model) Init() tea.Cmd { return tea.Batch(textinput.Blink, m.spinner.Tick) }
