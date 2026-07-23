package cmd

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/issy20/reporepo/internal/clients"
	"github.com/issy20/reporepo/internal/core"
	"github.com/issy20/reporepo/internal/store"
	"github.com/issy20/reporepo/internal/tui"
)

type stubAIClient struct{}

func (stubAIClient) Generate(context.Context, *core.RepoMeta, string, string) (*core.Analysis, error) {
	return nil, nil
}

func TestRunApplicationUsesOnlyAvailableOpenAIProvider(t *testing.T) {
	loaded := &core.Config{OpenAIAPIKey: "openai-key", DefaultProvider: "claude"}
	claudeCalls, openAICalls := 0, 0

	err := runApplicationWith(applicationDependencies{
		loadConfig: func() (*core.Config, error) { return loaded, nil },
		dataPath:   func() (string, error) { return filepath.Join(t.TempDir(), "data.json"), nil },
		newHTTP:    func() *http.Client { return &http.Client{} },
		newStore:   func(path string) *store.Store { return store.NewStore(path) },
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
	cfg := &core.Config{GithubToken: "github", AnthropicAPIKey: "anthropic", OpenAIAPIKey: "openai", DefaultProvider: "claude"}
	called := false
	err := runApplicationWith(applicationDependencies{
		loadConfig: func() (*core.Config, error) { return cfg, nil },
		dataPath:   func() (string, error) { return filepath.Join(t.TempDir(), "data.json"), nil },
		runTUI: func(deps tui.Dependencies, got *core.Config) error {
			called = true
			if got == cfg || *got != *cfg || deps.Store == nil || deps.GitHub == nil || deps.AI["claude"] == nil || deps.AI["openai"] == nil || deps.Now == nil {
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
			loadConfig: func() (*core.Config, error) { return &core.Config{AnthropicAPIKey: "key"}, nil },
			dataPath:   func() (string, error) { return filepath.Join(t.TempDir(), "data.json"), nil },
			runTUI:     func(tui.Dependencies, *core.Config) error { return want },
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
			return &core.Config{AnthropicAPIKey: " \t", OpenAIAPIKey: "\n"}, nil
		},
		dataPath: func() (string, error) {
			pathCalls++
			return "", nil
		},
	})
	if err == nil || err.Error() != "ANTHROPIC_API_KEY または OPENAI_API_KEY を設定してください" {
		t.Fatalf("error = %v", err)
	}
	if pathCalls != 0 {
		t.Fatalf("data path calls = %d, want 0", pathCalls)
	}
}

func TestRunApplicationSharesFiniteHTTPClientAndFactoryArguments(t *testing.T) {
	cfg := &core.Config{
		GithubToken: " github-token ", AnthropicAPIKey: " claude-key ", OpenAIAPIKey: " openai-key ",
		DefaultProvider: "openai",
	}
	httpCalls := 0
	shared := &http.Client{Timeout: applicationHTTPTimeout}
	path := filepath.Join(t.TempDir(), "data.json")

	err := runApplicationWith(applicationDependencies{
		loadConfig: func() (*core.Config, error) { return cfg, nil },
		dataPath:   func() (string, error) { return path, nil },
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
