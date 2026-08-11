package cmd

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/issy20/reporepo/internal/clients"
	"github.com/issy20/reporepo/internal/core"
	"github.com/issy20/reporepo/internal/secretstore"
	"github.com/issy20/reporepo/internal/store"
	"github.com/issy20/reporepo/internal/testutil"
	"github.com/issy20/reporepo/internal/tui"
)

type stubAIClient struct{}

func (stubAIClient) Generate(context.Context, *core.RepoMeta, string, string, string) (*core.Analysis, error) {
	return nil, nil
}

type stubGitHubClient struct {
	meta          *core.RepoMeta
	err           error
	trending      []clients.TrendingRepo
	trendingErr   error
	trendingQuery *clients.TrendingQuery
}

func (s stubGitHubClient) FetchRepository(_ context.Context, _, _ string) (*clients.RepositoryData, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &clients.RepositoryData{Meta: s.meta, README: "", Code: nil}, nil
}

func (s stubGitHubClient) FetchRepositoryMeta(_ context.Context, _, _ string) (*core.RepoMeta, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.meta, nil
}

func (s stubGitHubClient) SearchTrending(_ context.Context, q clients.TrendingQuery) ([]clients.TrendingRepo, error) {
	if s.trendingQuery != nil {
		*s.trendingQuery = q
	}
	if s.trendingErr != nil {
		return nil, s.trendingErr
	}
	return s.trending, nil
}

func TestRunApplicationUsesGeminiAsOnlyAvailableProvider(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	factoryCalls := 0
	err := runApplicationWith(applicationDependencies{
		loadConfig: func() (*core.Config, error) {
			return &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"}, nil
		},
		secretStore: testutil.NewMemorySecretStore(map[secretstore.Key]string{secretstore.GeminiAPIKey: "gemini-key"}),
		dataPath:    func() (string, error) { return filepath.Join(t.TempDir(), "data.json"), nil },
		newGemini: func(key, model string) (clients.AIClient, error) {
			factoryCalls++
			if key != "gemini-key" || model != defaultGeminiModel {
				t.Fatalf("Gemini factory args = %q, %q", key, model)
			}
			return stubAIClient{}, nil
		},
		runTUI: func(deps tui.Dependencies, cfg *core.Config) error {
			if cfg.DefaultProvider != "gemini" || len(deps.AI) != 1 || deps.AI["gemini"] == nil {
				t.Fatalf("TUI config = %#v, AI = %#v", cfg, deps.AI)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("runApplicationWith() error = %v", err)
	}
	if factoryCalls != 1 {
		t.Fatalf("Gemini factory calls = %d, want 1", factoryCalls)
	}
}

func TestRunApplicationResolvesSecretsFromStore(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	secretStore := testutil.NewMemorySecretStore(map[secretstore.Key]string{
		secretstore.GitHubToken:     "stored-github",
		secretstore.AnthropicAPIKey: "stored-anthropic",
	})

	err := runApplicationWith(applicationDependencies{
		loadConfig:  func() (*core.Config, error) { return &core.Config{DefaultProvider: "claude"}, nil },
		secretStore: secretStore,
		dataPath:    func() (string, error) { return filepath.Join(t.TempDir(), "data.json"), nil },
		newGitHub: func(_ *http.Client, _ string, token string) clients.GitHubClient {
			if token != "stored-github" {
				t.Fatal("GitHub factory did not receive stored token")
			}
			return nil
		},
		newClaude: func(key, _ string, _ *http.Client) clients.AIClient {
			if key != "stored-anthropic" {
				t.Fatal("Claude factory did not receive stored API key")
			}
			return stubAIClient{}
		},
		runTUI: func(tui.Dependencies, *core.Config) error { return nil },
	})
	if err != nil {
		t.Fatalf("runApplicationWith() error = %v", err)
	}
}

func TestRunApplicationMigratesLegacySecretsBeforeResolvingRuntime(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	secretStore := testutil.NewMemorySecretStore(nil)
	saveCalls := 0

	err := runApplicationWith(applicationDependencies{
		loadConfigFile: func() (*core.Config, core.LegacySecrets, error) {
			return &core.Config{DefaultProvider: "claude"}, core.LegacySecrets{AnthropicAPIKey: "legacy-anthropic"}, nil
		},
		saveConfig: func(cfg *core.Config) error {
			saveCalls++
			if cfg.AnthropicAPIKey != "" {
				t.Fatal("migration save contains legacy secret")
			}
			return nil
		},
		secretStore: secretStore,
		dataPath:    func() (string, error) { return filepath.Join(t.TempDir(), "data.json"), nil },
		newClaude: func(key, _ string, _ *http.Client) clients.AIClient {
			if key != "legacy-anthropic" {
				t.Fatal("runtime did not resolve migrated secret from Store")
			}
			return stubAIClient{}
		},
		runTUI: func(tui.Dependencies, *core.Config) error { return nil },
	})
	if err != nil {
		t.Fatalf("runApplicationWith() error = %v", err)
	}
	if saveCalls != 1 {
		t.Fatalf("migration save calls = %d, want 1", saveCalls)
	}
}

func TestRunApplicationReturnsSafeMigrationGuidance(t *testing.T) {
	const sensitive = "legacy-sensitive-value"
	secretStore := testutil.NewMemorySecretStore(nil)
	secretStore.FailSetAt = 1
	err := runApplicationWith(applicationDependencies{
		loadConfigFile: func() (*core.Config, core.LegacySecrets, error) {
			return &core.Config{}, core.LegacySecrets{AnthropicAPIKey: sensitive}, nil
		},
		saveConfig:  func(*core.Config) error { return nil },
		secretStore: secretStore,
	})
	if err == nil || !strings.Contains(err.Error(), "環境変数") {
		t.Fatalf("runApplicationWith() error = %v, want migration guidance", err)
	}
	if strings.Contains(err.Error(), sensitive) {
		t.Fatal("migration guidance contains legacy secret")
	}
}

func TestRunApplicationEnvironmentOnlySucceedsWhenStoreUnavailable(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "env-github")
	t.Setenv("ANTHROPIC_API_KEY", "env-anthropic")
	t.Setenv("OPENAI_API_KEY", "env-openai")
	t.Setenv("GEMINI_API_KEY", "env-gemini")
	backendFailure := errors.New("keychain unavailable")
	secretStore := testutil.NewMemorySecretStore(nil)
	secretStore.GetErrors = map[secretstore.Key]error{
		secretstore.GitHubToken: backendFailure, secretstore.AnthropicAPIKey: backendFailure, secretstore.OpenAIAPIKey: backendFailure,
	}

	err := runApplicationWith(applicationDependencies{
		loadConfig:  func() (*core.Config, error) { return &core.Config{DefaultProvider: "claude"}, nil },
		secretStore: secretStore,
		dataPath:    func() (string, error) { return filepath.Join(t.TempDir(), "data.json"), nil },
		runTUI:      func(tui.Dependencies, *core.Config) error { return nil },
	})
	if err != nil {
		t.Fatalf("runApplicationWith() error = %v", err)
	}
	if got := operationKeys(secretStore.Calls, "Get"); len(got) != 0 {
		t.Fatalf("Store.Get() calls = %v, want none", got)
	}
}

func TestRunApplicationPassesWarningsToConfiguredWarn(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	secretStore := testutil.NewMemorySecretStore(map[secretstore.Key]string{
		secretstore.AnthropicAPIKey: "anthropic",
	})
	secretStore.GetErrors = map[secretstore.Key]error{
		secretstore.GitHubToken: errors.New("backend down"),
	}
	var warnings []string
	err := runApplicationWith(applicationDependencies{
		loadConfig:  func() (*core.Config, error) { return &core.Config{DefaultProvider: "claude"}, nil },
		secretStore: secretStore,
		dataPath:    func() (string, error) { return filepath.Join(t.TempDir(), "data.json"), nil },
		warn:        func(msg string) { warnings = append(warnings, msg) },
		runTUI:      func(tui.Dependencies, *core.Config) error { return nil },
	})
	if err != nil {
		t.Fatalf("runApplicationWith() error = %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "GitHub tokenをOS資格情報ストアから読み込めませんでした") {
		t.Fatalf("warnings = %v, want secret resolution warning via deps.warn", warnings)
	}
}

func TestBuildRuntimeFallsBackToGHTokenWhenUnset(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	ghCalls := 0
	rt, err := buildRuntime(applicationDependencies{
		loadConfig: func() (*core.Config, error) {
			return &core.Config{DefaultProvider: "claude"}, nil
		},
		secretStore: testutil.NewMemorySecretStore(map[secretstore.Key]string{
			secretstore.AnthropicAPIKey: "anthropic",
		}),
		dataPath: func() (string, error) { return filepath.Join(t.TempDir(), "data.json"), nil },
		ghAuthToken: func() (string, error) {
			ghCalls++
			return "gh-token", nil
		},
		newGitHub: func(_ *http.Client, _ string, token string) clients.GitHubClient {
			if token != "gh-token" {
				t.Fatalf("GitHub token = %q, want gh-token", token)
			}
			return nil
		},
		newClaude: func(string, string, *http.Client) clients.AIClient { return stubAIClient{} },
	}, nil, true)
	if err != nil {
		t.Fatalf("buildRuntime() error = %v", err)
	}
	if ghCalls != 1 {
		t.Fatalf("ghAuthToken calls = %d, want 1", ghCalls)
	}
	if rt == nil {
		t.Fatalf("buildRuntime() = nil")
	}
}

func TestBuildRuntimeGHTokenIsTrimmed(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	_, err := buildRuntime(applicationDependencies{
		loadConfig: func() (*core.Config, error) {
			return &core.Config{DefaultProvider: "claude"}, nil
		},
		secretStore: testutil.NewMemorySecretStore(map[secretstore.Key]string{
			secretstore.AnthropicAPIKey: "anthropic",
		}),
		dataPath:    func() (string, error) { return filepath.Join(t.TempDir(), "data.json"), nil },
		ghAuthToken: func() (string, error) { return "  gh-token\n", nil },
		newGitHub: func(_ *http.Client, _ string, token string) clients.GitHubClient {
			if token != "gh-token" {
				t.Fatalf("GitHub token = %q, want trimmed gh-token", token)
			}
			return nil
		},
		newClaude: func(string, string, *http.Client) clients.AIClient { return stubAIClient{} },
	}, nil, true)
	if err != nil {
		t.Fatalf("buildRuntime() error = %v", err)
	}
}

func TestBuildRuntimeGHTokenNotCalledWhenEnvSet(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "env-github")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	ghCalls := 0
	_, err := buildRuntime(applicationDependencies{
		loadConfig: func() (*core.Config, error) {
			return &core.Config{DefaultProvider: "claude"}, nil
		},
		secretStore: testutil.NewMemorySecretStore(map[secretstore.Key]string{
			secretstore.AnthropicAPIKey: "anthropic",
		}),
		dataPath: func() (string, error) { return filepath.Join(t.TempDir(), "data.json"), nil },
		ghAuthToken: func() (string, error) {
			ghCalls++
			return "gh-token", nil
		},
		newGitHub: func(_ *http.Client, _ string, token string) clients.GitHubClient {
			if token != "env-github" {
				t.Fatalf("GitHub token = %q, want env-github", token)
			}
			return nil
		},
		newClaude: func(string, string, *http.Client) clients.AIClient { return stubAIClient{} },
	}, nil, true)
	if err != nil {
		t.Fatalf("buildRuntime() error = %v", err)
	}
	if ghCalls != 0 {
		t.Fatalf("ghAuthToken calls = %d, want 0", ghCalls)
	}
}

func TestBuildRuntimeGHTokenNotCalledWhenStored(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	ghCalls := 0
	_, err := buildRuntime(applicationDependencies{
		loadConfig: func() (*core.Config, error) {
			return &core.Config{DefaultProvider: "claude"}, nil
		},
		secretStore: testutil.NewMemorySecretStore(map[secretstore.Key]string{
			secretstore.GitHubToken:     "stored-github",
			secretstore.AnthropicAPIKey: "anthropic",
		}),
		dataPath: func() (string, error) { return filepath.Join(t.TempDir(), "data.json"), nil },
		ghAuthToken: func() (string, error) {
			ghCalls++
			return "gh-token", nil
		},
		newGitHub: func(_ *http.Client, _ string, token string) clients.GitHubClient {
			if token != "stored-github" {
				t.Fatalf("GitHub token = %q, want stored-github", token)
			}
			return nil
		},
		newClaude: func(string, string, *http.Client) clients.AIClient { return stubAIClient{} },
	}, nil, true)
	if err != nil {
		t.Fatalf("buildRuntime() error = %v", err)
	}
	if ghCalls != 0 {
		t.Fatalf("ghAuthToken calls = %d, want 0", ghCalls)
	}
}

func TestBuildRuntimeGHTokenFailureIsUnset(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	_, err := buildRuntime(applicationDependencies{
		loadConfig: func() (*core.Config, error) {
			return &core.Config{DefaultProvider: "claude"}, nil
		},
		secretStore: testutil.NewMemorySecretStore(map[secretstore.Key]string{
			secretstore.AnthropicAPIKey: "anthropic",
		}),
		dataPath:    func() (string, error) { return filepath.Join(t.TempDir(), "data.json"), nil },
		ghAuthToken: func() (string, error) { return "", errors.New("gh unavailable") },
		newClaude:   func(string, string, *http.Client) clients.AIClient { return stubAIClient{} },
	}, nil, true)
	if err != nil {
		t.Fatalf("buildRuntime() error = %v, want success with unset GitHub token", err)
	}
}

func TestBuildRuntimeGHTokenNotPersisted(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	store := testutil.NewMemorySecretStore(map[secretstore.Key]string{
		secretstore.AnthropicAPIKey: "anthropic",
	})
	_, err := buildRuntime(applicationDependencies{
		loadConfig: func() (*core.Config, error) {
			return &core.Config{DefaultProvider: "claude"}, nil
		},
		secretStore: store,
		dataPath:    func() (string, error) { return filepath.Join(t.TempDir(), "data.json"), nil },
		ghAuthToken: func() (string, error) { return "gh-token", nil },
		newClaude:   func(string, string, *http.Client) clients.AIClient { return stubAIClient{} },
	}, nil, true)
	if err != nil {
		t.Fatalf("buildRuntime() error = %v", err)
	}
	if _, exists := store.Snapshot()[secretstore.GitHubToken]; exists {
		t.Fatal("gh token was persisted to secret store")
	}
	for _, call := range store.Calls {
		if call.Method == "Set" {
			t.Fatalf("secret store received Set during runtime build: %v", call)
		}
	}
}

func TestBuildRuntimeGHTokenDoesNotSatisfyAIKeys(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	ghCalls := 0
	_, err := buildRuntime(applicationDependencies{
		loadConfig: func() (*core.Config, error) {
			return &core.Config{DefaultProvider: "claude"}, nil
		},
		secretStore: testutil.NewMemorySecretStore(nil),
		dataPath:    func() (string, error) { return filepath.Join(t.TempDir(), "data.json"), nil },
		ghAuthToken: func() (string, error) {
			ghCalls++
			return "gh-token", nil
		},
	}, nil, true)
	if err == nil || err.Error() != "ANTHROPIC_API_KEY、OPENAI_API_KEY、GEMINI_API_KEY のいずれかを設定してください" {
		t.Fatalf("error = %v, want AI key guidance", err)
	}
	if ghCalls != 0 {
		t.Fatalf("ghAuthToken calls = %d, want 0", ghCalls)
	}
}

func TestRunApplicationGHTokenFallback(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	ghCalls := 0
	err := runApplicationWith(applicationDependencies{
		loadConfig: func() (*core.Config, error) {
			return &core.Config{DefaultProvider: "claude"}, nil
		},
		secretStore: testutil.NewMemorySecretStore(map[secretstore.Key]string{
			secretstore.AnthropicAPIKey: "anthropic",
		}),
		dataPath: func() (string, error) { return filepath.Join(t.TempDir(), "data.json"), nil },
		ghAuthToken: func() (string, error) {
			ghCalls++
			return "gh-token", nil
		},
		newGitHub: func(_ *http.Client, _ string, token string) clients.GitHubClient {
			if token != "gh-token" {
				t.Fatalf("GitHub token = %q, want gh-token", token)
			}
			return nil
		},
		newClaude: func(string, string, *http.Client) clients.AIClient { return stubAIClient{} },
		runTUI:    func(tui.Dependencies, *core.Config) error { return nil },
	})
	if err != nil {
		t.Fatalf("runApplicationWith() error = %v", err)
	}
	if ghCalls != 1 {
		t.Fatalf("ghAuthToken calls = %d, want 1", ghCalls)
	}
}

func TestRunApplicationUsesOnlyAvailableOpenAIProvider(t *testing.T) {
	loaded := &core.Config{DefaultProvider: "claude"}
	claudeCalls, openAICalls := 0, 0

	err := runApplicationWith(applicationDependencies{
		loadConfig: func() (*core.Config, error) { return loaded, nil },
		secretStore: testutil.NewMemorySecretStore(map[secretstore.Key]string{
			secretstore.OpenAIAPIKey: "openai-key",
		}),
		dataPath: func() (string, error) { return filepath.Join(t.TempDir(), "data.json"), nil },
		newHTTP:  func() *http.Client { return &http.Client{} },
		newStore: func(path string) *store.Store { return store.NewStore(path) },
		newGitHub: func(*http.Client, string, string) clients.GitHubClient {
			return nil
		},
		newClaude: func(string, string, *http.Client) clients.AIClient {
			claudeCalls++
			return stubAIClient{}
		},
		newOpenAI: func(key, _ string, _ *http.Client) clients.AIClient {
			openAICalls++
			if key != "openai-key" {
				t.Fatalf("OpenAI key = %q", key)
			}
			return stubAIClient{}
		},
		runTUI: func(deps tui.Dependencies, cfg *core.Config) error {
			if cfg.DefaultProvider != "openai" {
				t.Fatalf("DefaultProvider = %q, want openai", cfg.DefaultProvider)
			}
			if len(deps.AI) != 1 || deps.AI["openai"] == nil {
				t.Fatalf("AI dependencies = %#v", deps.AI)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("runApplicationWith() error = %v", err)
	}
	if claudeCalls != 0 || openAICalls != 1 {
		t.Fatalf("factory calls: claude=%d openai=%d", claudeCalls, openAICalls)
	}
	if loaded.DefaultProvider != "claude" {
		t.Fatalf("loader-owned config was changed: %#v", loaded)
	}
}

func TestRunApplicationBuildsTUIDependencies(t *testing.T) {
	cfg := &core.Config{DefaultProvider: "claude"}
	called := false
	err := runApplicationWith(applicationDependencies{
		loadConfig: func() (*core.Config, error) { return cfg, nil },
		secretStore: testutil.NewMemorySecretStore(map[secretstore.Key]string{
			secretstore.GitHubToken:     "github",
			secretstore.AnthropicAPIKey: "anthropic",
			secretstore.OpenAIAPIKey:    "openai",
		}),
		dataPath: func() (string, error) { return filepath.Join(t.TempDir(), "data.json"), nil },
		runTUI: func(deps tui.Dependencies, got *core.Config) error {
			called = true
			if got == cfg || got.DefaultProvider != cfg.DefaultProvider || deps.Store == nil || deps.GitHub == nil || deps.AI["claude"] == nil || deps.AI["openai"] == nil || deps.Now == nil {
				t.Fatalf("incomplete TUI dependencies: %#v", deps)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("runApplicationWith() error = %v", err)
	}
	if !called {
		t.Fatal("TUI was not started")
	}
}

func TestRunApplicationReturnsSetupErrors(t *testing.T) {
	t.Run("config load", func(t *testing.T) {
		want := errors.New("load failed")
		err := runApplicationWith(applicationDependencies{loadConfig: func() (*core.Config, error) { return nil, want }})
		if err == nil || err.Error() != "設定を読み込めませんでした" || errors.Is(err, want) {
			t.Fatalf("error = %v, want safe config error", err)
		}
	})

	t.Run("TUI startup", func(t *testing.T) {
		want := errors.New("TUI failed")
		err := runApplicationWith(applicationDependencies{
			loadConfig: func() (*core.Config, error) { return &core.Config{}, nil },
			secretStore: testutil.NewMemorySecretStore(map[secretstore.Key]string{
				secretstore.AnthropicAPIKey: "key",
			}),
			dataPath: func() (string, error) { return filepath.Join(t.TempDir(), "data.json"), nil },
			runTUI:   func(tui.Dependencies, *core.Config) error { return want },
		})
		if err == nil || err.Error() != "TUIを起動できませんでした" || errors.Is(err, want) {
			t.Fatalf("error = %v, want safe TUI error", err)
		}
	})
}

func TestRunApplicationRejectsMissingAIKeysBeforeResolvingDataPath(t *testing.T) {
	pathCalls := 0
	err := runApplicationWith(applicationDependencies{
		loadConfig: func() (*core.Config, error) {
			return &core.Config{}, nil
		},
		secretStore: testutil.NewMemorySecretStore(nil),
		dataPath: func() (string, error) {
			pathCalls++
			return "", nil
		},
	})
	if err == nil || err.Error() != "ANTHROPIC_API_KEY、OPENAI_API_KEY、GEMINI_API_KEY のいずれかを設定してください" {
		t.Fatalf("error = %v", err)
	}
	if pathCalls != 0 {
		t.Fatalf("data path calls = %d, want 0", pathCalls)
	}
}

func TestRunApplicationSharesFiniteHTTPClientAndFactoryArguments(t *testing.T) {
	cfg := &core.Config{DefaultProvider: "openai"}
	httpCalls := 0
	shared := &http.Client{Timeout: applicationHTTPTimeout}
	path := filepath.Join(t.TempDir(), "data.json")

	err := runApplicationWith(applicationDependencies{
		loadConfig: func() (*core.Config, error) { return cfg, nil },
		secretStore: testutil.NewMemorySecretStore(map[secretstore.Key]string{
			secretstore.GitHubToken:     " github-token ",
			secretstore.AnthropicAPIKey: " claude-key ",
			secretstore.OpenAIAPIKey:    " openai-key ",
		}),
		dataPath: func() (string, error) { return path, nil },
		newHTTP: func() *http.Client {
			httpCalls++
			return shared
		},
		newStore: func(got string) *store.Store {
			if got != path {
				t.Fatalf("store path = %q", got)
			}
			return store.NewStore(got)
		},
		newGitHub: func(got *http.Client, baseURL, token string) clients.GitHubClient {
			if got != shared || baseURL != githubAPIURL || token != "github-token" {
				t.Fatalf("GitHub args = %p, %q, %q", got, baseURL, token)
			}
			return nil
		},
		newClaude: func(key, model string, got *http.Client) clients.AIClient {
			if got != shared || key != "claude-key" || model != defaultClaudeModel {
				t.Fatalf("Claude args = %p, %q, %q", got, key, model)
			}
			return stubAIClient{}
		},
		newOpenAI: func(key, model string, got *http.Client) clients.AIClient {
			if got != shared || key != "openai-key" || model != defaultOpenAIModel {
				t.Fatalf("OpenAI args = %p, %q, %q", got, key, model)
			}
			return stubAIClient{}
		},
		runTUI: func(tui.Dependencies, *core.Config) error { return nil },
	})
	if err != nil {
		t.Fatalf("runApplicationWith() error = %v", err)
	}
	if httpCalls != 1 {
		t.Fatalf("HTTP factory calls = %d, want 1", httpCalls)
	}
	if shared.Timeout <= 0 {
		t.Fatalf("HTTP timeout = %v", shared.Timeout)
	}
}

func TestBuildRuntimeBuildsSameClientsAsRun(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	secretStore := testutil.NewMemorySecretStore(map[secretstore.Key]string{
		secretstore.GitHubToken:     "github",
		secretstore.AnthropicAPIKey: "anthropic",
		secretstore.OpenAIAPIKey:    "openai",
	})
	path := filepath.Join(t.TempDir(), "data.json")
	httpCalls := 0
	shared := &http.Client{Timeout: applicationHTTPTimeout}

	rt, err := buildRuntime(applicationDependencies{
		loadConfig: func() (*core.Config, error) {
			return &core.Config{DefaultProvider: "openai", DefaultLanguage: "en"}, nil
		},
		secretStore: secretStore,
		dataPath:    func() (string, error) { return path, nil },
		newHTTP: func() *http.Client {
			httpCalls++
			return shared
		},
		newStore: func(got string) *store.Store {
			if got != path {
				t.Fatalf("store path = %q", got)
			}
			return store.NewStore(got)
		},
		newGitHub: func(_ *http.Client, baseURL, token string) clients.GitHubClient {
			if baseURL != githubAPIURL || token != "github" {
				t.Fatalf("GitHub args = %q, %q", baseURL, token)
			}
			return stubGitHubClient{}
		},
		newClaude: func(key, model string, got *http.Client) clients.AIClient {
			if key != "anthropic" || model != defaultClaudeModel || got != shared {
				t.Fatalf("Claude args = %q, %q, %p", key, model, got)
			}
			return stubAIClient{}
		},
		newOpenAI: func(key, model string, got *http.Client) clients.AIClient {
			if key != "openai" || model != defaultOpenAIModel || got != shared {
				t.Fatalf("OpenAI args = %q, %q, %p", key, model, got)
			}
			return stubAIClient{}
		},
	}, nil, true)
	if err != nil {
		t.Fatalf("buildRuntime() error = %v", err)
	}
	if rt == nil || rt.cfg == nil || rt.github == nil || rt.ai == nil || rt.store == nil {
		t.Fatalf("buildRuntime() = %#v", rt)
	}
	if rt.cfg.DefaultProvider != "openai" || rt.cfg.DefaultLanguage != "en" {
		t.Fatalf("runtime config = %#v", rt.cfg)
	}
	if len(rt.ai) != 2 || rt.ai["claude"] == nil || rt.ai["openai"] == nil || rt.ai["gemini"] != nil {
		t.Fatalf("runtime AI map = %#v", rt.ai)
	}
	if httpCalls != 1 {
		t.Fatalf("HTTP factory calls = %d, want 1", httpCalls)
	}
}

func TestBuildRuntimeReturnsSafeSetupErrors(t *testing.T) {
	t.Run("config load", func(t *testing.T) {
		_, err := buildRuntime(applicationDependencies{loadConfig: func() (*core.Config, error) { return nil, errors.New("secret") }}, nil, true)
		if err == nil || err.Error() != "設定を読み込めませんでした" || strings.Contains(err.Error(), "secret") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("secret resolution", func(t *testing.T) {
		_, err := buildRuntime(applicationDependencies{
			loadConfig:  func() (*core.Config, error) { return &core.Config{}, nil },
			secretStore: testutil.NewMemorySecretStore(nil),
		}, nil, true)
		if err == nil || err.Error() != "ANTHROPIC_API_KEY、OPENAI_API_KEY、GEMINI_API_KEY のいずれかを設定してください" {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("data path", func(t *testing.T) {
		_, err := buildRuntime(applicationDependencies{
			loadConfig: func() (*core.Config, error) { return &core.Config{}, nil },
			secretStore: testutil.NewMemorySecretStore(map[secretstore.Key]string{
				secretstore.AnthropicAPIKey: "key",
			}),
			dataPath: func() (string, error) { return "", errors.New("secret") },
		}, nil, true)
		if err == nil || err.Error() != "データ保存先を解決できませんでした" || strings.Contains(err.Error(), "secret") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestResolveDataPath(t *testing.T) {
	t.Run("XDG", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "/xdg/data")
		got, err := resolveDataPath(func() (string, error) { return "/home/user", nil })
		if err != nil || got != "/xdg/data/reporepo/data.json" {
			t.Fatalf("resolveDataPath() = %q, %v", got, err)
		}
	})

	t.Run("home fallback", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "")
		got, err := resolveDataPath(func() (string, error) { return "/home/user", nil })
		if err != nil || got != "/home/user/.local/share/reporepo/data.json" {
			t.Fatalf("resolveDataPath() = %q, %v", got, err)
		}
	})
}
