// Package analyzer は TUI と CLI が共有する解析パイプラインを提供する。
package analyzer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/issy20/reporepo/internal/clients"
	"github.com/issy20/reporepo/internal/core"
)

// Store は解析結果の永続化先を表す境界。
type Store interface {
	Load() ([]*core.Entry, error)
	Save([]*core.Entry) error
	Upsert(*core.Entry) error
}

// Analyzer はリポジトリ解析の共有パイプライン。
type Analyzer struct {
	store  Store
	github clients.GitHubClient
	ai     map[string]clients.AIClient
	now    func() time.Time
}

// New は Analyzer を構築する。
func New(store Store, github clients.GitHubClient, ai map[string]clients.AIClient, now func() time.Time) *Analyzer {
	if now == nil {
		now = time.Now
	}
	return &Analyzer{store: store, github: github, ai: ai, now: now}
}

// Analyze は入力から解析までを実行し、更新済みのエントリを返す。
func (a *Analyzer) Analyze(ctx context.Context, input, language, provider string, force bool) (*core.Entry, error) {
	owner, repo, err := clients.ParseRepositoryInput(input)
	if err != nil {
		return nil, errors.New("リポジトリの入力形式が正しくありません")
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if a.store == nil {
		return nil, errors.New("保存先を利用できません")
	}
	entries, err := a.store.Load()
	if err != nil {
		return nil, errors.New("履歴を読み込めませんでした")
	}
	fullName := owner + "/" + repo
	existing := FindEntry(entries, fullName)
	if !force && existing != nil && cacheMatches(existing.Analyses[language], provider, a.ai[provider]) {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		updated := CloneEntry(existing)
		updated.ViewedAt = a.now()
		if err := a.store.Upsert(updated); err != nil {
			return nil, errors.New("履歴を保存できませんでした")
		}
		return updated, nil
	}
	aiClient, ok := a.ai[provider]
	if !ok || aiClient == nil {
		return nil, fmt.Errorf("AI provider %q は利用できません", provider)
	}
	if a.github == nil {
		return nil, errors.New("GitHub client を利用できません")
	}
	data, err := a.github.FetchRepository(ctx, owner, repo)
	if err != nil {
		if contextError(ctx) != nil {
			return nil, errors.New("解析をキャンセルしました")
		}
		return nil, errors.New("GitHub からリポジトリ情報を取得できませんでした")
	}
	if data == nil || data.Meta == nil {
		return nil, errors.New("GitHub の応答にリポジトリ情報がありません")
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	analysis, err := aiClient.Generate(ctx, data.Meta, data.README, formatCodeContext(data.Code), language)
	if err != nil {
		if contextError(ctx) != nil {
			return nil, errors.New("解析をキャンセルしました")
		}
		return nil, errors.New("AI による解析に失敗しました")
	}
	if analysis == nil {
		return nil, errors.New("AI の応答に解析結果がありません")
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	now := a.now()
	entry := CloneEntry(existing)
	if entry == nil {
		entry = &core.Entry{FullName: data.Meta.FullName, CreatedAt: now, Analyses: make(map[string]*core.Analysis)}
		if strings.TrimSpace(entry.FullName) == "" {
			entry.FullName = fullName
		}
	}
	if entry.Analyses == nil {
		entry.Analyses = make(map[string]*core.Analysis)
	}
	entry.RepoMeta = data.Meta
	entry.Analyses[language] = analysis
	entry.ViewedAt = now
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if err := a.store.Upsert(entry); err != nil {
		return nil, errors.New("解析結果を保存できませんでした")
	}
	return entry, nil
}

func cacheMatches(analysis *core.Analysis, provider string, client clients.AIClient) bool {
	if analysis == nil {
		return false
	}
	// 入力定義が変わった古い解析（PromptVersion 不一致）はキャッシュ一致としない。
	if analysis.PromptVersion != clients.PromptVersion {
		return false
	}
	if analysis.Provider != provider {
		return false
	}
	identity, ok := client.(clients.AIIdentity)
	if !ok {
		return true
	}
	wantProvider, wantModel := identity.ProviderModel()
	return analysis.Provider == wantProvider && analysis.Model == wantModel
}

// CloneEntry はエントリの浅い複製を作る。
func CloneEntry(entry *core.Entry) *core.Entry {
	if entry == nil {
		return nil
	}
	cloned := *entry
	if entry.Analyses != nil {
		cloned.Analyses = make(map[string]*core.Analysis, len(entry.Analyses))
		for language, analysis := range entry.Analyses {
			cloned.Analyses[language] = analysis
		}
	}
	return &cloned
}

// FindEntry は大文字小文字を無視して fullName に一致するエントリを返す。
func FindEntry(entries []*core.Entry, fullName string) *core.Entry {
	for _, entry := range entries {
		if entry != nil && strings.EqualFold(entry.FullName, fullName) {
			return entry
		}
	}
	return nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return errors.New("解析をキャンセルしました")
	default:
		return nil
	}
}

// formatCodeContext はコード文脈を AI 入力用の "path: content" 文字列へ整形する。
func formatCodeContext(code *clients.CodeContext) string {
	if code == nil {
		return ""
	}
	parts := make([]string, 0, len(code.Files))
	for _, f := range code.Files {
		parts = append(parts, f.Path+":\n"+f.Content)
	}
	return strings.Join(parts, "\n")
}
