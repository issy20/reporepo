package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/issy20/reporepo/internal/clients"
	"github.com/issy20/reporepo/internal/core"
	"github.com/issy20/reporepo/internal/secretstore"
	"github.com/issy20/reporepo/internal/store"
	"github.com/issy20/reporepo/internal/trendingcache"
	"github.com/issy20/reporepo/internal/tui"
)

const (
	githubAPIURL           = "https://api.github.com"
	defaultClaudeModel     = "claude-sonnet-4-6"
	defaultOpenAIModel     = "gpt-4o-mini"
	defaultGeminiModel     = "gemini-3.5-flash"
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
	newGemini      func(string, string) (clients.AIClient, error)
	ghAuthToken    func() (string, error)
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
		newGemini: func(key, model string) (clients.AIClient, error) {
			return clients.NewGeminiClient(key, model)
		},
		ghAuthToken: ghCLIToken,
		runTUI: func(deps tui.Dependencies, cfg *core.Config) error {
			return tui.Run(deps, cfg)
		},
	}
}

// runtime は run と analyze、trending が共有する実行時オブジェクト群。
type runtime struct {
	cfg      *core.Config
	github   clients.GitHubClient
	ai       map[string]clients.AIClient
	store    *store.Store
	dataPath string
}

func buildRuntime(deps applicationDependencies, warn func(string), requireAI bool) (*runtime, error) {
	defaults := defaultApplicationDependencies()
	if warn == nil {
		warn = defaults.warn
	}
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
	if deps.newGemini == nil {
		deps.newGemini = defaults.newGemini
	}
	if deps.loadConfig == nil && deps.loadConfigFile == nil {
		return nil, errors.New("設定の読み込み処理を利用できません")
	}
	var cfg *core.Config
	var err error
	if deps.loadConfigFile != nil {
		var legacy core.LegacySecrets
		cfg, legacy, err = deps.loadConfigFile()
		if err == nil {
			err = migrateLegacySecrets(cfg, legacy, deps.secretStore, deps.saveConfig)
			if isMigrationFailure(err) {
				return nil, err
			}
		}
	} else {
		cfg, err = deps.loadConfig()
	}
	if err != nil {
		return nil, errors.New("設定を読み込めませんでした")
	}
	if cfg == nil {
		return nil, errors.New("設定を読み込めませんでした")
	}

	runtimeConfig, warnings, err := resolveRuntimeSecrets(cfg, deps.secretStore, requireAI)
	for _, warning := range warnings {
		warn(warning)
	}
	if err != nil {
		return nil, err
	}
	if runtimeConfig.GithubToken == "" && deps.ghAuthToken != nil {
		if token, ghErr := deps.ghAuthToken(); ghErr == nil && strings.TrimSpace(token) != "" {
			runtimeConfig.GithubToken = strings.TrimSpace(token)
		}
	}

	path, err := deps.dataPath()
	if err != nil {
		return nil, errors.New("データ保存先を解決できませんでした")
	}

	httpClient := deps.newHTTP()
	rt := &runtime{
		cfg:      runtimeConfig,
		github:   deps.newGitHub(httpClient, githubAPIURL, runtimeConfig.GithubToken),
		ai:       make(map[string]clients.AIClient, 3),
		store:    deps.newStore(path),
		dataPath: path,
	}

	hasClaude := runtimeConfig.AnthropicAPIKey != ""
	hasOpenAI := runtimeConfig.OpenAIAPIKey != ""
	hasGemini := runtimeConfig.GeminiAPIKey != ""
	if hasClaude {
		rt.ai["claude"] = deps.newClaude(runtimeConfig.AnthropicAPIKey, defaultClaudeModel, httpClient)
	}
	if hasOpenAI {
		rt.ai["openai"] = deps.newOpenAI(runtimeConfig.OpenAIAPIKey, defaultOpenAIModel, httpClient)
	}
	if hasGemini {
		geminiClient, err := deps.newGemini(runtimeConfig.GeminiAPIKey, defaultGeminiModel)
		if err != nil {
			return nil, errors.New("Gemini clientを初期化できませんでした")
		}
		rt.ai["gemini"] = geminiClient
	}
	return rt, nil
}

func runApplicationWith(deps applicationDependencies) error {
	rt, err := buildRuntime(deps, deps.warn, false)
	if err != nil {
		return err
	}
	if deps.runTUI == nil {
		return errors.New("TUIを起動できませんでした")
	}
	tuiDeps := tui.Dependencies{
		Store:             rt.store,
		GitHub:            rt.github,
		AI:                rt.ai,
		Now:               time.Now,
		TrendingCachePath: trendingcache.Path(rt.dataPath),
	}
	if err := deps.runTUI(tuiDeps, rt.cfg); err != nil {
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
