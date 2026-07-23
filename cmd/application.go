package cmd

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/issy20/reporepo/internal/clients"
	"github.com/issy20/reporepo/internal/core"
	"github.com/issy20/reporepo/internal/store"
	"github.com/issy20/reporepo/internal/tui"
)

const (
	githubAPIURL           = "https://api.github.com"
	defaultClaudeModel     = "claude-sonnet-4-6"
	defaultOpenAIModel     = "gpt-4o-mini"
	applicationHTTPTimeout = 2 * time.Minute
)

type applicationDependencies struct {
	loadConfig func() (*core.Config, error)
	dataPath   func() (string, error)
	newHTTP    func() *http.Client
	newStore   func(string) *store.Store
	newGitHub  func(*http.Client, string, string) clients.GitHubClient
	newClaude  func(string, string, *http.Client) clients.AIClient
	newOpenAI  func(string, string, *http.Client) clients.AIClient
	runTUI     func(tui.Dependencies, *core.Config) error
}

func runApplication() error {
	return runApplicationWith(defaultApplicationDependencies())
}

func defaultApplicationDependencies() applicationDependencies {
	return applicationDependencies{
		loadConfig: core.LoadConfig,
		dataPath:   dataFilePath,
		newHTTP:    func() *http.Client { return &http.Client{Timeout: applicationHTTPTimeout} },
		newStore:   store.NewStore,
		newGitHub: func(client *http.Client, baseURL, token string) clients.GitHubClient {
			return clients.NewGitHubClient(client, baseURL, token)
		},
		newClaude: func(key, model string, client *http.Client) clients.AIClient {
			return clients.NewClaudeClient(key, model, client)
		},
		newOpenAI: func(key, model string, client *http.Client) clients.AIClient {
			return clients.NewOpenAIClient(key, model, client)
		},
		runTUI: func(deps tui.Dependencies, cfg *core.Config) error {
			return tui.Run(deps, cfg)
		},
	}
}

func runApplicationWith(deps applicationDependencies) error {
	defaults := defaultApplicationDependencies()
	if deps.dataPath == nil {
		deps.dataPath = defaults.dataPath
	}
	if deps.newHTTP == nil {
		deps.newHTTP = defaults.newHTTP
	}
	if deps.newStore == nil {
		deps.newStore = defaults.newStore
	}
	if deps.newGitHub == nil {
		deps.newGitHub = defaults.newGitHub
	}
	if deps.newClaude == nil {
		deps.newClaude = defaults.newClaude
	}
	if deps.newOpenAI == nil {
		deps.newOpenAI = defaults.newOpenAI
	}
	if deps.loadConfig == nil {
		return errors.New("設定の読み込み処理を利用できません")
	}
	cfg, err := deps.loadConfig()
	if err != nil {
		return errors.New("設定を読み込めませんでした")
	}
	if cfg == nil {
		return errors.New("設定を読み込めませんでした")
	}

	runtimeConfig := *cfg
	runtimeConfig.GithubToken = strings.TrimSpace(runtimeConfig.GithubToken)
	runtimeConfig.AnthropicAPIKey = strings.TrimSpace(runtimeConfig.AnthropicAPIKey)
	runtimeConfig.OpenAIAPIKey = strings.TrimSpace(runtimeConfig.OpenAIAPIKey)
	hasClaude := runtimeConfig.AnthropicAPIKey != ""
	hasOpenAI := runtimeConfig.OpenAIAPIKey != ""
	if !hasClaude && !hasOpenAI {
		return errors.New("ANTHROPIC_API_KEY または OPENAI_API_KEY を設定してください")
	}
	if (runtimeConfig.DefaultProvider == "claude" && !hasClaude) ||
		(runtimeConfig.DefaultProvider == "openai" && !hasOpenAI) ||
		(runtimeConfig.DefaultProvider != "claude" && runtimeConfig.DefaultProvider != "openai") {
		if hasClaude {
			runtimeConfig.DefaultProvider = "claude"
		} else {
			runtimeConfig.DefaultProvider = "openai"
		}
	}

	path, err := deps.dataPath()
	if err != nil {
		return errors.New("データ保存先を解決できませんでした")
	}
	if deps.runTUI == nil {
		return errors.New("TUIを起動できませんでした")
	}

	httpClient := deps.newHTTP()
	ai := make(map[string]clients.AIClient, 2)
	if hasClaude {
		ai["claude"] = deps.newClaude(runtimeConfig.AnthropicAPIKey, defaultClaudeModel, httpClient)
	}
	if hasOpenAI {
		ai["openai"] = deps.newOpenAI(runtimeConfig.OpenAIAPIKey, defaultOpenAIModel, httpClient)
	}
	tuiDeps := tui.Dependencies{
		Store:  deps.newStore(path),
		GitHub: deps.newGitHub(httpClient, githubAPIURL, runtimeConfig.GithubToken),
		AI:     ai,
		Now:    time.Now,
	}
	if err := deps.runTUI(tuiDeps, &runtimeConfig); err != nil {
		return errors.New("TUIを起動できませんでした")
	}
	return nil
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
