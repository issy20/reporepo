package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/issy20/reporepo/internal/core"
)

type fakeRenderer struct {
	output string
	err    error
}

func (r fakeRenderer) Render(string, int) (string, error) { return r.output, r.err }

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

func TestNewProgramAndInit(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, nil)
	if m.Init() == nil {
		t.Fatal("Init returned nil")
	}
	if NewProgram(Dependencies{Store: &fakeStore{}}, nil) == nil {
		t.Fatal("NewProgram returned nil")
	}
}
