package cmd

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/issy20/reporepo/internal/core"
)

func TestRunConfigWizard_NoDoesNotSave(t *testing.T) {
	saves := 0
	in := strings.NewReader("\n\n\n\n\nno\n")
	out := &bytes.Buffer{}
	err := runConfigWizard(in, out,
		func() (*core.Config, error) {
			return &core.Config{AnthropicAPIKey: "stored-key", DefaultProvider: "claude", DefaultLanguage: "ja"}, nil
		},
		func(*core.Config) error { saves++; return nil },
	)
	if err != nil {
		t.Fatalf("runConfigWizard() error = %v", err)
	}
	if saves != 0 {
		t.Fatalf("save calls = %d, want 0", saves)
	}
}

func TestRunConfigWizard_UpdatesAndSavesAfterYes(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	original := &core.Config{GithubToken: "old-token", AnthropicAPIKey: "old-key", OpenAIAPIKey: "remove-me", DefaultProvider: "claude", DefaultLanguage: "ja"}
	var saved *core.Config
	out := &bytes.Buffer{}
	err := runConfigWizard(strings.NewReader(" new-token \n new-key \n-\nclaude\nen\nyes\n"), out,
		func() (*core.Config, error) { return original, nil },
		func(cfg *core.Config) error { copy := *cfg; saved = &copy; return nil },
	)
	if err != nil {
		t.Fatalf("runConfigWizard() error = %v", err)
	}
	if saved == nil {
		t.Fatal("configuration was not saved")
	}
	if saved.GithubToken != "new-token" || saved.AnthropicAPIKey != "new-key" || saved.OpenAIAPIKey != "" || saved.DefaultProvider != "claude" || saved.DefaultLanguage != "en" {
		t.Fatalf("saved config = %#v", saved)
	}
	if original.GithubToken != "old-token" || original.OpenAIAPIKey != "remove-me" {
		t.Fatalf("source config was mutated: %#v", original)
	}
	for _, secret := range []string{"old-token", "old-key", "remove-me", "new-token", "new-key"} {
		if strings.Contains(out.String(), secret) {
			t.Fatalf("output contains secret %q: %s", secret, out.String())
		}
	}
}

func TestRunConfigWizard_EnvironmentKeyIsNotSaved(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "github-environment-secret")
	t.Setenv("ANTHROPIC_API_KEY", "environment-secret")
	var saved *core.Config
	out := &bytes.Buffer{}
	err := runConfigWizard(strings.NewReader("\n\n\n\n\nY\n"), out,
		func() (*core.Config, error) {
			return &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"}, nil
		},
		func(cfg *core.Config) error { copy := *cfg; saved = &copy; return nil },
	)
	if err != nil {
		t.Fatalf("runConfigWizard() error = %v", err)
	}
	if saved == nil || saved.GithubToken != "" || saved.AnthropicAPIKey != "" {
		t.Fatalf("saved config = %#v", saved)
	}
	if strings.Contains(out.String(), "environment-secret") || strings.Contains(out.String(), "github-environment-secret") {
		t.Fatal("environment secret leaked to output")
	}
	if !strings.Contains(out.String(), "GITHUB_TOKEN") || !strings.Contains(out.String(), "環境変数で設定済み") {
		t.Fatalf("environment status is missing: %s", out.String())
	}
}

func TestRunConfigWizard_InvalidChoicesAreRetried(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	var saved *core.Config
	out := &bytes.Buffer{}
	err := runConfigWizard(strings.NewReader("\nkey\n\ninvalid\nclaude\nfr\nja\ny\n"), out,
		func() (*core.Config, error) {
			return &core.Config{DefaultProvider: "broken", DefaultLanguage: "broken"}, nil
		},
		func(cfg *core.Config) error { saved = cfg; return nil },
	)
	if err != nil {
		t.Fatalf("runConfigWizard() error = %v", err)
	}
	if saved == nil || saved.DefaultProvider != "claude" || saved.DefaultLanguage != "ja" {
		t.Fatalf("saved config = %#v", saved)
	}
	if !strings.Contains(out.String(), "claude, openai") || !strings.Contains(out.String(), "ja, en") {
		t.Fatalf("validation output = %s", out.String())
	}
}

func TestRunConfigWizard_EOFDoesNotSave(t *testing.T) {
	saves := 0
	err := runConfigWizard(strings.NewReader(""), io.Discard,
		func() (*core.Config, error) { return &core.Config{}, nil },
		func(*core.Config) error { saves++; return nil },
	)
	if err != nil {
		t.Fatalf("runConfigWizard() error = %v", err)
	}
	if saves != 0 {
		t.Fatalf("save calls = %d", saves)
	}
}

func TestRunConfigWizard_SanitizesLoadAndSaveErrors(t *testing.T) {
	secret := "must-not-leak"
	loadErr := runConfigWizard(strings.NewReader(""), io.Discard,
		func() (*core.Config, error) { return nil, errors.New(secret) },
		func(*core.Config) error { return nil },
	)
	if loadErr == nil || strings.Contains(loadErr.Error(), secret) {
		t.Fatalf("load error = %v", loadErr)
	}

	t.Setenv("ANTHROPIC_API_KEY", "env-key")
	saveErr := runConfigWizard(strings.NewReader("\n\n\n\n\ny\n"), io.Discard,
		func() (*core.Config, error) {
			return &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"}, nil
		},
		func(*core.Config) error { return errors.New(secret) },
	)
	if saveErr == nil || strings.Contains(saveErr.Error(), secret) {
		t.Fatalf("save error = %v", saveErr)
	}
}
