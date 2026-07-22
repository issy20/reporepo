package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/issy20/reporepo/internal/clients"
	"github.com/issy20/reporepo/internal/core"
	"github.com/issy20/reporepo/internal/store"
	"github.com/issy20/reporepo/internal/tui"
)

const (
	githubAPIURL       = "https://api.github.com"
	defaultClaudeModel = "claude-sonnet-4-6"
	defaultOpenAIModel = "gpt-4o-mini"
)

type applicationDependencies struct {
	loadConfig func() (*core.Config, error)
	dataPath   func() (string, error)
	runTUI     func(tui.Dependencies, *core.Config) error
}

func runApplication() error {
	return runApplicationWith(applicationDependencies{
		loadConfig: core.LoadConfig,
		dataPath:   dataFilePath,
		runTUI: func(deps tui.Dependencies, cfg *core.Config) error {
			return tui.Run(deps, cfg)
		},
	})
}

func runApplicationWith(deps applicationDependencies) error {
	if deps.loadConfig == nil {
		return errors.New("設定の読み込み処理を利用できません")
	}
	cfg, err := deps.loadConfig()
	if err != nil {
		return fmt.Errorf("設定を読み込めません: %w", err)
	}
	if cfg == nil {
		return errors.New("設定を読み込めません: 結果が空です")
	}
	if deps.dataPath == nil {
		return errors.New("データ保存先を解決できません")
	}
	path, err := deps.dataPath()
	if err != nil {
		return fmt.Errorf("データ保存先を解決できません: %w", err)
	}
	if deps.runTUI == nil {
		return errors.New("TUI 起動処理を利用できません")
	}

	tuiDeps := tui.Dependencies{
		Store:  store.NewStore(path),
		GitHub: clients.NewGitHubClient(http.DefaultClient, githubAPIURL, cfg.GithubToken),
		AI: map[string]clients.AIClient{
			"claude": clients.NewClaudeClient(cfg.AnthropicAPIKey, defaultClaudeModel, http.DefaultClient),
			"openai": clients.NewOpenAIClient(cfg.OpenAIAPIKey, defaultOpenAIModel, http.DefaultClient),
		},
		Now: time.Now,
	}
	return deps.runTUI(tuiDeps, cfg)
}

func dataFilePath() (string, error) {
	return resolveDataPath(os.UserHomeDir)
}

func resolveDataPath(userHomeDir func() (string, error)) (string, error) {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "reporepo", "data.json"), nil
	}
	home, err := userHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "reporepo", "data.json"), nil
}
