package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/issy20/reporepo/internal/analyzer"
	"github.com/issy20/reporepo/internal/core"
)

// formatAnalyzePlain は解析結果を ANSI を含まない plain text へ整形する。
func formatAnalyzePlain(result *analyzer.Result, language string, now time.Time) string {
	entry := result.Entry
	if entry == nil {
		return "解析結果がありません"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", safeText(entry.FullName))
	if entry.RepoMeta != nil {
		fmt.Fprintf(&b, "⭐ %d  Forks %d  Language %s\n", entry.RepoMeta.Stars, entry.RepoMeta.Forks, safeText(entry.RepoMeta.Language))
	}
	a := entry.Analyses[language]
	if a == nil {
		b.WriteString("解析結果がありません")
		return b.String()
	}
	fetched := "不明"
	if entry.RepoMeta != nil && !entry.RepoMeta.FetchedAt.IsZero() {
		fetched = relativeDay(entry.RepoMeta.FetchedAt, now)
	}
	fmt.Fprintf(&b, "取得: %s  解析: %s\n\n", fetched, relativeDay(a.CreatedAt, now))
	b.WriteString("# Summary\n")
	b.WriteString(safeText(a.Summary))
	b.WriteString("\n# Tech Stack\n")
	b.WriteString(safeText(a.TechStack))
	b.WriteString("\n# Background\n")
	b.WriteString(safeText(a.Background))
	b.WriteString("\n# Keywords\n")
	keywords := make([]string, len(a.Keywords))
	for i, keyword := range a.Keywords {
		keywords[i] = safeText(keyword)
	}
	b.WriteString(strings.Join(keywords, ", "))
	b.WriteByte('\n')
	if a.IsStale(entry.RepoMeta) {
		b.WriteString("解析はリポジトリ更新前のものです（--force で再生成）\n")
	}
	return b.String()
}

type analyzeJSONOutput struct {
	FullName  string          `json:"full_name"`
	Repo      *core.RepoMeta  `json:"repo"`
	Analysis  *analysisOutput `json:"analysis"`
	Language  string          `json:"language"`
	Provider  string          `json:"provider"`
	Model     string          `json:"model"`
	CreatedAt time.Time       `json:"created_at"`
	Stale     bool            `json:"stale"`
}

type analysisOutput struct {
	Summary    string   `json:"summary"`
	TechStack  string   `json:"tech_stack"`
	Background string   `json:"background"`
	Keywords   []string `json:"keywords"`
}

func writeAnalyzeJSON(out io.Writer, result *analyzer.Result, language string) error {
	entry := result.Entry
	output := analyzeJSONOutput{Language: language}
	if entry != nil {
		output.FullName = entry.FullName
		output.Repo = entry.RepoMeta
		analysis := entry.Analyses[language]
		if analysis != nil {
			output.Analysis = &analysisOutput{
				Summary:    analysis.Summary,
				TechStack:  analysis.TechStack,
				Background: analysis.Background,
				Keywords:   analysis.Keywords,
			}
			output.Provider = analysis.Provider
			output.Model = analysis.Model
			output.CreatedAt = analysis.CreatedAt
			output.Stale = analysis.IsStale(entry.RepoMeta)
		}
	}
	encoder := json.NewEncoder(out)
	return encoder.Encode(output)
}

// relativeDay は現在との日数差を「今日」「N日前」等で返す。ゼロ値は「不明」。
func relativeDay(t, now time.Time) string {
	if t.IsZero() {
		return "不明"
	}
	days := int(now.Sub(t).Hours() / 24)
	if days < 0 {
		days = 0
	}
	if days == 0 {
		return "今日"
	}
	return fmt.Sprintf("%d日前", days)
}

// safeText は制御文字を除去して plain text として安全にする。
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
