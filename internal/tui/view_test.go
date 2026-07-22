package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/issy20/reporepo/internal/core"
)

type fakeRenderer struct {
	output string
	err    error
}

func TestInputHistoryFitsHeightAndFollowsSelection(t *testing.T) {
	entries := make([]*core.Entry, 10)
	for i := range entries {
		entries[i] = &core.Entry{FullName: fmt.Sprintf("owner/repo-%02d", i)}
	}
	m := NewModel(Dependencies{Store: &fakeStore{entries: entries}}, nil)
	m.width, m.height, m.selected = 80, 10, 9
	view := m.View()
	if strings.Contains(view, "owner/repo-00") || !strings.Contains(view, "owner/repo-09") {
		t.Fatalf("history window did not follow selection:\n%s", view)
	}
	if got := strings.Count(view, "owner/repo-"); got > 2 {
		t.Fatalf("rendered %d history rows at height %d:\n%s", got, m.height, view)
	}
}

func TestInputErrorReducesAvailableHistoryHeight(t *testing.T) {
	entries := make([]*core.Entry, 10)
	for i := range entries {
		entries[i] = &core.Entry{FullName: fmt.Sprintf("owner/repo-%02d", i)}
	}
	m := NewModel(Dependencies{Store: &fakeStore{entries: entries}}, nil)
	m.width, m.height, m.errMessage = 80, 12, "error"
	view := m.View()
	if got := strings.Count(view, "owner/repo-"); got > 2 {
		t.Fatalf("rendered %d history rows with error at height %d:\n%s", got, m.height, view)
	}
}

func TestInputViewSanitizesAndTruncatesExternalLines(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	m.width, m.height = 16, 20
	m.visible = []*core.Entry{nil, {FullName: "所有者/とても長いリポジトリ\x1b[31m", IsFavorite: true}}
	m.selected = 1
	m.errMessage = "長い\x07エラーメッセージです"
	view := m.View()
	if strings.ContainsAny(view, "\x07\x1b") {
		t.Fatalf("unsafe control character in view: %q", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > m.width {
			t.Fatalf("line width=%d exceeds %d: %q", got, m.width, line)
		}
	}
}

func TestLoadingViewUsesDefaultAndConstrainsUnsafeLabel(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	m.state, m.width, m.height = stateLoading, 12, 1
	if got := m.View(); got == "" || !strings.Contains(got, "解析") {
		t.Fatalf("default loading view=%q", got)
	}
	m.loadingLabel = "解析しています: とても長い\x1b[31m名前"
	view := m.View()
	if strings.Contains(view, "\x1b[31m") {
		t.Fatalf("unsafe label=%q", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > m.width {
			t.Fatalf("line width=%d exceeds %d: %q", got, m.width, line)
		}
	}
}

func TestLoadingViewIsNonEmptyAtOneCell(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	m.state, m.width, m.height = stateLoading, 1, 1
	if got := strings.TrimSpace(m.View()); got == "" {
		t.Fatal("loading view is empty at 1x1")
	}
}

func TestDetailEmptyStateFitsNarrowTerminal(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	m.state, m.width, m.height, m.current = stateDetail, 10, 4, nil
	view := m.View()
	if !strings.Contains(view, "戻る") {
		t.Fatalf("missing back guidance: %q", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > m.width {
			t.Fatalf("line width=%d exceeds %d: %q", got, m.width, line)
		}
	}
}

func (r fakeRenderer) Render(string, int) (string, error) { return r.output, r.err }

type recordingRenderer struct {
	source string
	width  int
}

func (r *recordingRenderer) Render(source string, width int) (string, error) {
	r.source, r.width = source, width
	return source, nil
}

func TestViewsContainRequiredInformation(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	input := m.View()
	for _, want := range []string{"owner/repo", "履歴", "お気に入り", "言語: ja", "provider: claude", "Enter:"} {
		if !strings.Contains(input, want) {
			t.Errorf("input view missing %q", want)
		}
	}
	m.state = stateLoading
	m.loadingLabel = "解析しています: owner/repo"
	if got := m.View(); !strings.Contains(got, "owner/repo") || !strings.Contains(got, "キャンセル") {
		t.Errorf("loading view=%q", got)
	}
	m.state = stateDetail
	m.width = 80
	m.height = 24
	m.viewport.Width = 78
	m.viewport.Height = 21
	m.current = &core.Entry{FullName: "owner/repo", RepoMeta: &core.RepoMeta{Description: "description", Stars: 3}, Analyses: map[string]*core.Analysis{"ja": {Summary: "summary", TechStack: "Go", Background: "background", Keywords: []string{"tui"}}}}
	m.renderer = fakeRenderer{output: detailMarkdown(m.current, "ja")}
	m.setDetailContent()
	detail := m.View()
	for _, want := range []string{"owner/repo", "Summary", "summary", "Tech Stack", "Background", "Keywords"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail view missing %q: %q", want, detail)
		}
	}
}

func TestDetailFallsBackToPlainTextAndNilDoesNotPanic(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}, Renderer: fakeRenderer{err: errors.New("render")}}, nil)
	m.state = stateDetail
	m.width = 80
	m.height = 24
	m.viewport.Width = 78
	m.viewport.Height = 21
	m.current = &core.Entry{FullName: "owner/repo", RepoMeta: nil, Analyses: nil}
	m.setDetailContent()
	if got := m.View(); !strings.Contains(got, "owner/repo") {
		t.Fatalf("view=%q", got)
	}
	m.width, m.height, m.viewport.Width, m.viewport.Height = 1, 1, 1, 1
	_ = m.View()
	m.current = nil
	_ = m.View()
}

func TestDetailSanitizesExternalTextAndUsesSafeRenderWidth(t *testing.T) {
	renderer := &recordingRenderer{}
	m := NewModel(Dependencies{Store: &fakeStore{}, Renderer: renderer}, nil)
	m.width = 0
	m.current = &core.Entry{
		FullName: "owner/\x1b[31mrepo",
		RepoMeta: &core.RepoMeta{Description: "desc\x00ription", Language: "G\x7fo"},
		Analyses: map[string]*core.Analysis{"ja": {Summary: "sum\x1bmary", TechStack: "Go\tCLI", Background: "back\nline", Keywords: []string{"safe", "bad\x07word"}}},
	}
	m.setDetailContent()
	if strings.ContainsAny(renderer.source, "\x00\x07\x1b\x7f") {
		t.Fatalf("unsafe control character reached renderer: %q", renderer.source)
	}
	if !strings.Contains(renderer.source, "Go\tCLI") || !strings.Contains(renderer.source, "back\nline") || renderer.width < 1 {
		t.Fatalf("source=%q width=%d", renderer.source, renderer.width)
	}
}

func TestDetailEscapesMarkdownInRepositoryMetadata(t *testing.T) {
	renderer := &recordingRenderer{}
	m := NewModel(Dependencies{Store: &fakeStore{}, Renderer: renderer}, nil)
	m.current = &core.Entry{
		FullName: "owner/#repo_[x]",
		RepoMeta: &core.RepoMeta{Description: "**bold** [link](target)", Language: "C++"},
	}
	m.setDetailContent()
	for _, want := range []string{`owner/\#repo\_\[x\]`, `\*\*bold\*\* \[link\]\(target\)`, `C\+\+`} {
		if !strings.Contains(renderer.source, want) {
			t.Fatalf("metadata was not escaped; missing %q in %q", want, renderer.source)
		}
	}
}

func TestSelectionAndFavoriteStylesRemainSemanticallyDistinct(t *testing.T) {
	if !activeStyle.GetBold() || !selectedStyle.GetBold() {
		t.Fatal("active tab and selected history styles should be bold")
	}
	if favoriteStyle.GetForeground() == dimStyle.GetForeground() {
		t.Fatal("favorite marker should not use the dim help color")
	}
	selectedColor := selectedStyle.GetForeground()
	favoriteColor := favoriteStyle.GetForeground()
	m := NewModel(Dependencies{Store: &fakeStore{entries: []*core.Entry{{FullName: "owner/repo", IsFavorite: true}}}}, nil)
	m.errMessage = "error"
	view := m.View()
	for _, want := range []string{"> ", "★", "error"} {
		if !strings.Contains(view, want) {
			t.Fatalf("non-color marker %q missing in %q", want, view)
		}
	}
	if selectedStyle.GetForeground() != selectedColor || favoriteStyle.GetForeground() != favoriteColor {
		t.Fatal("rendering mutated shared styles")
	}
}

func TestInputViewStatesAndDoesNotMutateModel(t *testing.T) {
	first := &core.Entry{FullName: "first/repo", IsFavorite: true}
	second := &core.Entry{FullName: "second/repo"}
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	m.entries = []*core.Entry{first, second}
	m.visible = []*core.Entry{first, second}
	m.selected = 99
	entriesBefore := append([]*core.Entry(nil), m.entries...)
	visibleBefore := append([]*core.Entry(nil), m.visible...)
	view := m.View()
	for _, want := range []string{"履歴", "お気に入り", "★", "second/repo"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q in %q", want, view)
		}
	}
	if m.selected != 99 || m.entries[0] != entriesBefore[0] || m.entries[1] != entriesBefore[1] || m.visible[0] != visibleBefore[0] || m.visible[1] != visibleBefore[1] {
		t.Fatal("View mutated model state")
	}
	m.tab = tabFavorites
	if got := m.View(); !strings.Contains(got, "お気に入り") {
		t.Fatalf("favorites tab missing: %q", got)
	}
}

func TestUnknownScreenStateFallsBackToInput(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	m.state = screenState(255)
	if got := m.View(); !strings.Contains(got, "reporepo") || !strings.Contains(got, "Enter:") {
		t.Fatalf("unknown state view=%q", got)
	}
}

func TestDetailMarkdownContainsAllMetadataAndAnalysisFields(t *testing.T) {
	entry := &core.Entry{
		FullName: "所有者/repository",
		RepoMeta: &core.RepoMeta{Description: "説明", Stars: 12, Forks: 3, Language: "Go"},
		Analyses: map[string]*core.Analysis{"ja": {Summary: "要約", TechStack: "技術", Background: "背景", Keywords: []string{"一", "二"}}},
	}
	got := detailMarkdown(entry, "ja")
	for _, want := range []string{"所有者/repository", "説明", "12", "3", "Go", "要約", "技術", "背景", "一, 二"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	entry.Analyses["ja"].Keywords = nil
	if got := detailMarkdown(entry, "ja"); !strings.Contains(got, "## Keywords") {
		t.Fatalf("empty keywords removed section: %q", got)
	}
}

func TestRendererFallbacksDoNotExposeErrors(t *testing.T) {
	entry := &core.Entry{FullName: "owner/repo"}
	for name, renderer := range map[string]markdownRenderer{
		"nil":          nil,
		"empty":        fakeRenderer{},
		"secret error": fakeRenderer{err: errors.New("secret renderer failure")},
	} {
		t.Run(name, func(t *testing.T) {
			m := Model{current: entry, language: "ja", width: 0, viewport: viewport.New(80, 1), renderer: renderer}
			m.setDetailContent()
			got := m.viewport.View()
			if !strings.Contains(got, "owner/repo") || strings.Contains(got, "secret") {
				t.Fatalf("fallback view=%q", got)
			}
		})
	}
}

func TestDetailViewShowsHelpWithoutLargeVerticalOverflow(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}, Renderer: fakeRenderer{output: "body"}}, nil)
	m.state, m.current = stateDetail, &core.Entry{FullName: "owner/repo"}
	m.width, m.height = 40, 5
	m.viewport.Width, m.viewport.Height = 38, 2
	m.setDetailContent()
	view := m.View()
	if !strings.Contains(view, "Esc:") || strings.Count(view, "\n") > m.viewport.Height+1 {
		t.Fatalf("detail layout=%q", view)
	}
}

func TestNewProgramAndInit(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	if m.Init() == nil {
		t.Fatal("Init returned nil")
	}
	if NewProgram(Dependencies{Store: &fakeStore{}}, nil) == nil {
		t.Fatal("NewProgram returned nil")
	}
}
