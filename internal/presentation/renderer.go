// Package presentation はCLIの意味に基づく表示を提供する。
package presentation

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// Row はsummaryの一行分の表示データである。
type Row struct {
	Label string
	Value string
}

// Renderer は一つの出力先へCLI表示を書き込む。
type Renderer struct {
	out    io.Writer
	caps   Capabilities
	styles rendererStyles
}

type rendererStyles struct {
	title, section, success, warning, failure, hint, prompt lipgloss.Style
}

// NewRenderer は指定した能力でrendererを作る。
func NewRenderer(out io.Writer, caps Capabilities) *Renderer {
	if caps.Width <= 0 {
		caps.Width = defaultWidth
	}
	lr := lipgloss.NewRenderer(out)
	lr.SetColorProfile(termenv.ANSI256)
	return &Renderer{out: out, caps: caps, styles: rendererStyles{
		title:   lr.NewStyle().Bold(true).Foreground(lipgloss.Color("63")),
		section: lr.NewStyle().Bold(true).Foreground(lipgloss.Color("63")),
		success: lr.NewStyle().Foreground(lipgloss.Color("42")),
		warning: lr.NewStyle().Foreground(lipgloss.Color("214")),
		failure: lr.NewStyle().Foreground(lipgloss.Color("196")),
		hint:    lr.NewStyle().Faint(true).Foreground(lipgloss.Color("245")),
		prompt:  lr.NewStyle().Foreground(lipgloss.Color("63")),
	}}
}

// Decorated は装飾表示が有効かを返す。
func (r *Renderer) Decorated() bool { return r.caps.Decorated }

func clean(text string) string     { return ansi.Strip(text) }
func StripANSI(text string) string { return ansi.Strip(text) }

func (r *Renderer) line(plainPrefix, decoratedPrefix, text string, style lipgloss.Style) error {
	text = clean(text)
	if r.caps.Decorated {
		text = style.Render(decoratedPrefix + text)
	} else {
		text = plainPrefix + text
	}
	_, err := fmt.Fprintln(r.out, text)
	return err
}

func (r *Renderer) Title(text string) error   { return r.line("", "", text, r.styles.title) }
func (r *Renderer) Section(text string) error { return r.line("", "", text, r.styles.section) }
func (r *Renderer) Success(text string) error { return r.line("OK: ", "✓ ", text, r.styles.success) }
func (r *Renderer) Warning(text string) error {
	return r.line("WARNING: ", "⚠ ", text, r.styles.warning)
}
func (r *Renderer) Error(text string) error { return r.line("ERROR: ", "✗ ", text, r.styles.failure) }
func (r *Renderer) Hint(text string) error  { return r.line("", "", text, r.styles.hint) }

func (r *Renderer) Prompt(label string) error {
	label = clean(label)
	if r.caps.Decorated {
		label = r.styles.prompt.Render(label)
	}
	_, err := fmt.Fprint(r.out, label)
	return err
}

func (r *Renderer) Summary(rows []Row) error {
	maxLabel := 0
	for _, row := range rows {
		if n := len([]rune(clean(row.Label))); n > maxLabel {
			maxLabel = n
		}
	}
	for _, row := range rows {
		label, value := clean(row.Label), clean(row.Value)
		var line string
		if r.caps.Width < 40 {
			line = label + ": " + value
		} else {
			line = fmt.Sprintf("%-*s  %s", maxLabel, label, value)
		}
		if _, err := fmt.Fprintln(r.out, strings.TrimRight(line, " ")); err != nil {
			return err
		}
	}
	return nil
}
