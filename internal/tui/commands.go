package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/issy20/reporepo/internal/clients"
	"github.com/issy20/reporepo/internal/core"
)

type analysisSucceededMsg struct {
	requestID uint64
	entry     *core.Entry
}
type analysisFailedMsg struct {
	requestID uint64
	err       error
}
type entriesChangedMsg struct{ err error }

func (m Model) analyzeCmd(ctx context.Context, input string, force bool, requestID uint64) tea.Cmd {
	return func() tea.Msg {
		entry, err := m.analyze(ctx, input, force)
		if err != nil {
			return analysisFailedMsg{requestID: requestID, err: err}
		}
		return analysisSucceededMsg{requestID: requestID, entry: entry}
	}
}

func (m Model) analyze(ctx context.Context, input string, force bool) (*core.Entry, error) {
	owner, repo, err := clients.ParseRepositoryInput(input)
	if err != nil {
		return nil, errors.New("リポジトリの入力形式が正しくありません")
	}
	if m.store == nil {
		return nil, errors.New("保存先を利用できません")
	}
	entries, err := m.store.Load()
	if err != nil {
		return nil, errors.New("履歴を読み込めませんでした")
	}
	fullName := owner + "/" + repo
	existing := findEntry(entries, fullName)
	if !force && existing != nil && existing.Analyses[m.language] != nil {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		existing.ViewedAt = m.now()
		if err := m.store.Upsert(existing); err != nil {
			return nil, errors.New("履歴を保存できませんでした")
		}
		return existing, nil
	}
	aiClient, ok := m.ai[m.provider]
	if !ok || aiClient == nil {
		return nil, fmt.Errorf("AI provider %q は利用できません", m.provider)
	}
	if m.github == nil {
		return nil, errors.New("GitHub client を利用できません")
	}
	data, err := m.github.FetchRepository(ctx, owner, repo)
	if err != nil {
		return nil, errors.New("GitHub からリポジトリ情報を取得できませんでした")
	}
	if data == nil || data.Meta == nil {
		return nil, errors.New("GitHub の応答にリポジトリ情報がありません")
	}
	analysis, err := aiClient.Generate(ctx, data.Meta, data.README, m.language)
	if err != nil {
		return nil, errors.New("AI による解析に失敗しました")
	}
	if analysis == nil {
		return nil, errors.New("AI の応答に解析結果がありません")
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	now := m.now()
	entry := existing
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
	entry.Analyses[m.language] = analysis
	entry.ViewedAt = now
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if err := m.store.Upsert(entry); err != nil {
		return nil, errors.New("解析結果を保存できませんでした")
	}
	return entry, nil
}

func findEntry(entries []*core.Entry, fullName string) *core.Entry {
	for _, entry := range entries {
		if entry != nil && strings.EqualFold(entry.FullName, fullName) {
			return entry
		}
	}
	return nil
}

func contextError(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return errors.New("解析をキャンセルしました")
	default:
		return nil
	}
}
