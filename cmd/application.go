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
	"github.com/issy20/reporepo/internal/secretstore"
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
	loadConfig     func() (*core.Config, error)
	loadConfigFile func() (*core.Config, core.LegacySecrets, error)
	saveConfig     func(*core.Config) error
	secretStore    secretstore.Store
	warn           func(string)
	dataPath       func() (string, error)
	newHTTP        func() *http.Client
	newStore       func(string) *store.Store
	newGitHub      func(*http.Client, string, string) clients.GitHubClient
	newClaude      func(string, string, *http.Client) clients.AIClient
	newOpenAI      func(string, string, *http.Client) clients.AIClient
	runTUI         func(tui.Dependencies, *core.Config) error
}

func runApplication() error {
	return runApplicationWith(defaultApplicationDependencies())
}

func defaultApplicationDependencies() applicationDependencies {
	return applicationDependencies{
		loadConfigFile: core.LoadConfigFile,
		saveConfig:     core.SaveConfig,
		secretStore:    secretstore.NewKeyringStore(),
		warn: func(message string) {
			_, _ = fmt.Fprintln(os.Stderr, "警告:", message)
		},
		dataPath: dataFilePath,
		newHTTP:  func() *http.Client { return &http.Client{Timeout: applicationHTTPTimeout} },
		newStore: store.NewStore,
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
	if deps.warn == nil {
		deps.warn = defaults.warn
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
	if deps.loadConfig == nil && deps.loadConfigFile == nil {
		return errors.New("設定の読み込み処理を利用できません")
	}
	var cfg *core.Config
	var err error
	if deps.loadConfigFile != nil {
		var legacy core.LegacySecrets
		cfg, legacy, err = deps.loadConfigFile()
		if err == nil {
			err = migrateLegacySecrets(cfg, legacy, deps.secretStore, deps.saveConfig)
			if isMigrationFailure(err) {
				return err
			}
		}
	} else {
		cfg, err = deps.loadConfig()
	}
	if err != nil {
		return errors.New("設定を読み込めませんでした")
	}
	if cfg == nil {
		return errors.New("設定を読み込めませんでした")
	}

	runtimeConfig, warnings, err := resolveRuntimeSecrets(cfg, deps.secretStore)
	for _, warning := range warnings {
		deps.warn(warning)
	}
	if err != nil {
		return err
	}
	hasClaude := runtimeConfig.AnthropicAPIKey != ""
	hasOpenAI := runtimeConfig.OpenAIAPIKey != ""

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
	if err := deps.runTUI(tuiDeps, runtimeConfig); err != nil {
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
