package cmd

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/issy20/reporepo/internal/core"
	"github.com/issy20/reporepo/internal/tui"
)

func TestRunApplicationBuildsTUIDependencies(t *testing.T) {
	cfg := &core.Config{GithubToken: "github", AnthropicAPIKey: "anthropic", OpenAIAPIKey: "openai"}
	called := false
	err := runApplicationWith(applicationDependencies{
		loadConfig: func() (*core.Config, error) { return cfg, nil },
		dataPath:   func() (string, error) { return filepath.Join(t.TempDir(), "data.json"), nil },
		runTUI: func(deps tui.Dependencies, got *core.Config) error {
			called = true
			if got != cfg || deps.Store == nil || deps.GitHub == nil || deps.AI["claude"] == nil || deps.AI["openai"] == nil || deps.Now == nil {
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
		if !errors.Is(err, want) {
			t.Fatalf("error = %v, want %v", err, want)
		}
	})

	t.Run("TUI startup", func(t *testing.T) {
		want := errors.New("TUI failed")
		err := runApplicationWith(applicationDependencies{
			loadConfig: func() (*core.Config, error) { return &core.Config{}, nil },
			dataPath:   func() (string, error) { return filepath.Join(t.TempDir(), "data.json"), nil },
			runTUI:     func(tui.Dependencies, *core.Config) error { return want },
		})
		if !errors.Is(err, want) {
			t.Fatalf("error = %v, want %v", err, want)
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
