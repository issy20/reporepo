package cmd

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/issy20/reporepo/internal/core"
	"github.com/issy20/reporepo/internal/secretstore"
	"github.com/issy20/reporepo/internal/testutil"
)

func TestNonSecretCommandsDoNotAccessSecretStore(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"version"}, {"where"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			secretStore := testutil.NewMemorySecretStore(nil)
			root := newRootCommand(commandDependencies{
				run:         func() error { t.Fatal("run called"); return nil },
				secretStore: secretStore,
				configPath:  func() (string, error) { return "/config.json", nil },
				dataPath:    func() (string, error) { return "/data.json", nil },
			})
			root.SetOut(io.Discard)
			root.SetErr(io.Discard)
			root.SetArgs(args)
			if err := root.Execute(); err != nil {
				t.Fatalf("Execute(%v) error = %v", args, err)
			}
			if len(secretStore.Calls) != 0 {
				t.Fatalf("command %v accessed SecretStore", args)
			}
		})
	}
}

func TestWhereAndVersion(t *testing.T) {
	out := &bytes.Buffer{}
	root := newRootCommand(commandDependencies{
		run:        func() error { return nil },
		configPath: func() (string, error) { return "/config/config.json", nil },
		dataPath:   func() (string, error) { return "/data/data.json", nil },
	})
	root.SetOut(out)
	root.SetArgs([]string{"where"})
	if err := root.Execute(); err != nil {
		t.Fatalf("where error = %v", err)
	}
	if got := out.String(); !strings.Contains(got, "/config/config.json") || !strings.Contains(got, "/data/data.json") {
		t.Fatalf("where output = %q", got)
	}

	out.Reset()
	root = newRootCommand(commandDependencies{run: func() error { return nil }})
	root.SetOut(out)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("version error = %v", err)
	}
	if got := out.String(); !strings.Contains(got, version) {
		t.Fatalf("version output = %q", got)
	}
}

func TestConfigUpdatesValuesWithoutLeakingExistingSecrets(t *testing.T) {
	existing := &core.Config{
		GithubToken: "old-github-secret", AnthropicAPIKey: "old-anthropic-secret", OpenAIAPIKey: "old-openai-secret",
		DefaultProvider: "claude", DefaultLanguage: "ja",
	}
	var saved *core.Config
	secretStore := testutil.NewMemorySecretStore(map[secretstore.Key]string{
		secretstore.GitHubToken: "old-github-secret", secretstore.AnthropicAPIKey: "old-anthropic-secret", secretstore.OpenAIAPIKey: "old-openai-secret",
	})
	out := &bytes.Buffer{}
	root := newRootCommand(commandDependencies{
		run:         func() error { return nil },
		loadConfig:  func() (*core.Config, error) { return existing, nil },
		saveConfig:  func(cfg *core.Config) error { saved = cfg; return nil },
		secretStore: secretStore,
	})
	root.SetIn(strings.NewReader("\nnew-anthropic\n\ninvalid\nopenai\nxx\nen\ny\n"))
	root.SetOut(out)
	root.SetArgs([]string{"config"})

	if err := root.Execute(); err != nil {
		t.Fatalf("config error = %v", err)
	}
	if saved == nil {
		t.Fatal("config was not saved")
	}
	if saved.GithubToken != "" || saved.AnthropicAPIKey != "" || saved.OpenAIAPIKey != "" || saved.DefaultProvider != "openai" || saved.DefaultLanguage != "en" {
		t.Fatalf("saved config = %#v", saved)
	}
	if secretStore.Snapshot()[secretstore.AnthropicAPIKey] != "new-anthropic" {
		t.Fatal("updated secret was not saved to Keychain store")
	}
	for _, secret := range []string{"old-github-secret", "old-anthropic-secret", "old-openai-secret"} {
		if strings.Contains(out.String(), secret) {
			t.Fatalf("output leaked secret %q: %q", secret, out.String())
		}
	}
}
