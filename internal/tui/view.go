package tui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"
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
	layout := newLayout(m.width, m.height)
	m.input.Width = layout.inputWidth
	b.WriteString(titleStyle.Render("reporepo"))
	b.WriteString("\n\n")
	b.WriteString(fitLine(m.input.View(), layout.width))
	b.WriteString("\n\n")
	history, favorites := "履歴", "お気に入り"
	if m.tab == tabHistory {
		history = activeStyle.Render(history)
	} else {
		favorites = activeStyle.Render(favorites)
	}
	b.WriteString(fitLine(fmt.Sprintf("%s  %s", history, favorites), layout.width))
	b.WriteByte('\n')
	historyHeight := layout.historyHeight
	if m.errMessage != "" {
		historyHeight = max(0, historyHeight-2)
	}
	if len(m.visible) == 0 && historyHeight > 0 {
		b.WriteString(dimStyle.Render("  項目はありません"))
		b.WriteByte('\n')
	}
	start, end := visibleRange(len(m.visible), m.selected, historyHeight)
	for i := start; i < end; i++ {
		entry := m.visible[i]
		if entry == nil {
			continue
		}
		cursor := "  "
		if i == m.selected {
			cursor = "> "
		}
		favorite := ""
		if entry.IsFavorite {
			favorite = favoriteStyle.Render(" ★")
		}
		line := cursor + safeText(entry.FullName) + favorite
		if i == m.selected {
			line = selectedStyle.Render(line)
		}
		b.WriteString(fitLine(line, layout.width))
		b.WriteByte('\n')
	}
	b.WriteString("\n")
	b.WriteString(fitLine(fmt.Sprintf("言語: %s  provider: %s", m.language, m.provider), layout.width))
	b.WriteByte('\n')
	b.WriteString(fitLine(dimStyle.Render("Enter: 開く  ↑↓: 選択  Tab: タブ  f: お気に入り  d: 削除  l: 言語  p: provider  q/Esc: 終了"), layout.width))
	if m.errMessage != "" {
		b.WriteString("\n\n")
		b.WriteString(fitLine(errorStyle.Render(safeText(m.errMessage)), layout.width))
	}
	return b.String()
}

func fitLine(value string, width int) string {
	width = max(1, width)
	tail := ""
	if width > 1 {
		tail = "…"
	}
	return ansi.TruncateWc(value, width, tail)
}

func (m Model) viewLoading() string {
	label := safeText(m.loadingLabel)
	if label == "" {
		label = "解析しています"
	}
	width := newLayout(m.width, m.height).width
	return fmt.Sprintf("%s\n\n%s", fitLine(m.spinner.View()+" "+label, width), fitLine(dimStyle.Render("Esc: キャンセル"), width))
}

func (m Model) viewDetail() string {
	width := newLayout(m.width, m.height).width
	if m.current == nil {
		return fitLine("解析結果がありません", width) + "\n\n" + fitLine("Esc: 戻る", width)
	}
	return m.viewport.View() + "\n" + fitLine(dimStyle.Render("Esc: 戻る  ↑↓/PgUp/PgDn: スクロール  l: 言語  f: お気に入り  r: 再生成"), width)
}

func detailMarkdown(entry *core.Entry, language string) string {
	if entry == nil {
		return "解析結果がありません"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", safeMarkdownText(entry.FullName))
	if entry.RepoMeta != nil {
		fmt.Fprintf(&b, "%s\n\n⭐ %d  Forks %d  Language %s\n\n", safeMarkdownText(entry.RepoMeta.Description), entry.RepoMeta.Stars, entry.RepoMeta.Forks, safeMarkdownText(entry.RepoMeta.Language))
	}
	a := entry.Analyses[language]
	if a == nil {
		b.WriteString("解析結果がありません")
	} else {
		keywords := make([]string, len(a.Keywords))
		for i, keyword := range a.Keywords {
			keywords[i] = safeText(keyword)
		}
		fmt.Fprintf(&b, "## Summary\n%s\n\n## Tech Stack\n%s\n\n## Background\n%s\n\n## Keywords\n%s", safeText(a.Summary), safeText(a.TechStack), safeText(a.Background), strings.Join(keywords, ", "))
	}
	return b.String()
}

func safeText(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
}

var markdownEscaper = strings.NewReplacer(
	`\`, `\\`, "`", "\\`", "*", `\*`, "_", `\_`, "{", `\{`, "}", `\}`,
	"[", `\[`, "]", `\]`, "<", `\<`, ">", `\>`, "(", `\(`, ")", `\)`,
	"#", `\#`, "+", `\+`, "-", `\-`, ".", `\.`, "!", `\!`, "|", `\|`,
)

func safeMarkdownText(value string) string {
	return markdownEscaper.Replace(safeText(value))
}

func (m *Model) setDetailContent() {
	m.renderDetailContent()
	m.viewport.GotoTop()
}

func (m *Model) resizeDetailContent() {
	oldMax := max(0, m.viewport.TotalLineCount()-m.viewport.Height)
	ratio := 0.0
	if oldMax > 0 {
		ratio = float64(m.viewport.YOffset) / float64(oldMax)
	}
	m.renderDetailContent()
	newMax := max(0, m.viewport.TotalLineCount()-m.viewport.Height)
	m.viewport.SetYOffset(int(ratio*float64(newMax) + 0.5))
}

func (m *Model) renderDetailContent() {
	plain := detailMarkdown(m.current, m.language)
	content := plain
	var err error
	if m.renderer != nil {
		content, err = m.renderer.Render(plain, max(1, m.width-4))
	}
	if err != nil || content == "" {
		content = plain
	}
	m.viewport.SetContent(content)
}
