package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/issy20/reporepo/internal/core"
)

type markdownRenderer interface {
	Render(string, int) (string, error)
}

type glamourRenderer struct{}

func (glamourRenderer) Render(source string, width int) (string, error) {
	if width < 1 {
		width = 1
	}
	r, err := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(width))
	if err != nil {
		return "", err
	}
	return r.Render(source)
}

func (m Model) View() string {
	switch m.state {
	case stateLoading:
		return m.viewLoading()
	case stateDetail:
		return m.viewDetail()
	default:
		return m.viewInput()
	}
}

func (m Model) viewInput() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("reporepo"))
	b.WriteString("\n\n")
	b.WriteString(m.input.View())
	b.WriteString("\n\n")
	history, favorites := "履歴", "お気に入り"
	if m.tab == tabHistory {
		history = activeStyle.Render(history)
	} else {
		favorites = activeStyle.Render(favorites)
	}
	fmt.Fprintf(&b, "%s  %s\n", history, favorites)
	if len(m.visible) == 0 {
		b.WriteString(dimStyle.Render("  項目はありません"))
		b.WriteByte('\n')
	}
	for i, entry := range m.visible {
		cursor := "  "
		if i == m.selected {
			cursor = "> "
		}
		favorite := ""
		if entry.IsFavorite {
			favorite = " ★"
		}
		fmt.Fprintf(&b, "%s%s%s\n", cursor, entry.FullName, favorite)
	}
	fmt.Fprintf(&b, "\n言語: %s  provider: %s\n", m.language, m.provider)
	b.WriteString(dimStyle.Render("Enter: 開く  ↑↓: 選択  Tab: タブ  f: お気に入り  d: 削除  l: 言語  p: provider  q/Esc: 終了"))
	if m.errMessage != "" {
		b.WriteString("\n\n")
		b.WriteString(errorStyle.Render(m.errMessage))
	}
	return b.String()
}

func (m Model) viewLoading() string {
	label := m.loadingLabel
	if label == "" {
		label = "解析しています"
	}
	return fmt.Sprintf("%s %s\n\n%s", m.spinner.View(), label, dimStyle.Render("Esc: キャンセル"))
}

func (m Model) viewDetail() string {
	if m.current == nil {
		return "解析結果がありません\n\nEsc: 戻る"
	}
	return m.viewport.View() + "\n" + dimStyle.Render("↑↓/PgUp/PgDn: スクロール  l: 言語  f: お気に入り  r: 再生成  Esc: 戻る")
}

func detailMarkdown(entry *core.Entry, language string) string {
	if entry == nil {
		return "解析結果がありません"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", entry.FullName)
	if entry.RepoMeta != nil {
		fmt.Fprintf(&b, "%s\n\n⭐ %d  Forks %d  Language %s\n\n", entry.RepoMeta.Description, entry.RepoMeta.Stars, entry.RepoMeta.Forks, entry.RepoMeta.Language)
	}
	a := entry.Analyses[language]
	if a == nil {
		b.WriteString("解析結果がありません")
	} else {
		fmt.Fprintf(&b, "## Summary\n%s\n\n## Tech Stack\n%s\n\n## Background\n%s\n\n## Keywords\n%s", a.Summary, a.TechStack, a.Background, strings.Join(a.Keywords, ", "))
	}
	return b.String()
}

func (m *Model) setDetailContent() {
	plain := detailMarkdown(m.current, m.language)
	content, err := m.renderer.Render(plain, max(1, m.width-4))
	if err != nil {
		content = plain
	}
	m.viewport.SetContent(content)
	m.viewport.GotoTop()
}
