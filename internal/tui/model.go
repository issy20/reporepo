package tui

import (
	"context"
	"sort"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/issy20/reporepo/internal/analyzer"
	"github.com/issy20/reporepo/internal/clients"
	"github.com/issy20/reporepo/internal/core"
)

type screenState uint8

const (
	stateInput screenState = iota
	stateLoading
	stateDetail
	stateTrending
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
	Store             entryStore
	GitHub            clients.GitHubClient
	AI                map[string]clients.AIClient
	Now               func() time.Time
	Renderer          markdownRenderer
	TrendingCachePath string
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
	warnings     []string
	loadingLabel string

	store     entryStore
	github    clients.GitHubClient
	ai        map[string]clients.AIClient
	now       func() time.Time
	renderer  markdownRenderer
	analyzer  *analyzer.Analyzer
	cancel    context.CancelFunc
	requestID uint64

	mutationRequestID uint64
	mutationPending   bool

	trendingRepos     []clients.TrendingRepo
	trendingSelected  int
	trendingErr       string
	trendingStale     bool
	trendingLoading   bool
	trendingRequestID uint64
	trendingCachePath string
}

func NewModel(deps Dependencies, cfg *core.Config) Model {
	language, provider := "ja", "claude"
	if cfg != nil {
		if cfg.DefaultLanguage == "ja" || cfg.DefaultLanguage == "en" {
			language = cfg.DefaultLanguage
		}
		if cfg.DefaultProvider == "claude" || cfg.DefaultProvider == "openai" || cfg.DefaultProvider == "gemini" {
			provider = cfg.DefaultProvider
		}
	}
	available := availableProviders(deps.AI)
	if len(available) > 0 && !containsProvider(available, provider) {
		provider = available[0]
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
	layout := newLayout(80, 24)
	input.Width = layout.inputWidth
	m := Model{state: stateInput, input: input, spinner: sp, viewport: viewport.New(0, 0), width: layout.width, height: layout.height, language: language, provider: provider, store: deps.Store, github: deps.GitHub, ai: deps.AI, now: now, renderer: renderer, trendingCachePath: deps.TrendingCachePath}
	m.analyzer = analyzer.New(deps.Store, deps.GitHub, deps.AI, now, analyzer.DefaultRefreshInterval)
	m.reloadEntries()
	return m
}

func availableProviders(ai map[string]clients.AIClient) []string {
	providers := make([]string, 0, 3)
	for _, provider := range []string{"claude", "openai", "gemini"} {
		if ai[provider] != nil {
			providers = append(providers, provider)
		}
	}
	return providers
}

func containsProvider(providers []string, target string) bool {
	for _, provider := range providers {
		if provider == target {
			return true
		}
	}
	return false
}

func nextProvider(current string, available []string) string {
	if len(available) == 0 {
		return current
	}
	for i, provider := range available {
		if provider == current {
			return available[(i+1)%len(available)]
		}
	}
	return available[0]
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
	m.errMessage = ""
	m.entries = make([]*core.Entry, 0, len(entries))
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
