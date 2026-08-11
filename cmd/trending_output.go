package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/issy20/reporepo/internal/clients"
)

// writeTrendingOutput は一覧を plain または JSON で出力する。repo 名・説明は攻撃者制御データのためサニタイズする。
func writeTrendingOutput(out io.Writer, repos []clients.TrendingRepo, jsonOutput bool) error {
	if jsonOutput {
		sanitized := make([]clients.TrendingRepo, len(repos))
		for i, repo := range repos {
			sanitized[i] = clients.TrendingRepo{
				FullName:    safeText(repo.FullName),
				Description: safeText(repo.Description),
				Stars:       repo.Stars,
				Language:    safeText(repo.Language),
			}
		}
		return json.NewEncoder(out).Encode(sanitized)
	}
	_, err := fmt.Fprint(out, formatTrendingPlain(repos))
	return err
}

// formatTrendingPlain は1行1repoのplain textへ整形する。
func formatTrendingPlain(repos []clients.TrendingRepo) string {
	var b strings.Builder
	for _, repo := range repos {
		fmt.Fprintf(&b, "%s ⭐ %d  %s  %s\n", safeText(repo.FullName), repo.Stars, safeText(repo.Description), safeText(repo.Language))
	}
	return b.String()
}
