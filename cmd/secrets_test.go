package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/issy20/reporepo/internal/core"
	"github.com/issy20/reporepo/internal/secretstore"
	"github.com/issy20/reporepo/internal/testutil"
)

func TestResolveRuntimeSecretsEnvironmentTakesPriorityWithoutStoreGet(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", " env-github ")
	t.Setenv("ANTHROPIC_API_KEY", " env-anthropic ")
	t.Setenv("OPENAI_API_KEY", " env-openai ")
	t.Setenv("GEMINI_API_KEY", " env-gemini ")
	store := testutil.NewMemorySecretStore(map[secretstore.Key]string{
		secretstore.GitHubToken:     "stored-github",
		secretstore.AnthropicAPIKey: "stored-anthropic",
		secretstore.OpenAIAPIKey:    "stored-openai",
		secretstore.GeminiAPIKey:    "stored-gemini",
	})
	cfg := &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"}

	got, warnings, err := resolveRuntimeSecrets(cfg, store)
	if err != nil {
		t.Fatalf("resolveRuntimeSecrets() error = %v", err)
	}
	if got.GithubToken != "env-github" || got.AnthropicAPIKey != "env-anthropic" || got.OpenAIAPIKey != "env-openai" || got.GeminiAPIKey != "env-gemini" {
		t.Fatal("resolveRuntimeSecrets() did not use environment secrets")
	}
	if got := operationKeys(store.Calls, "Get"); len(got) != 0 {
		t.Fatalf("Store.Get() calls = %v, want none", got)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if cfg.GithubToken != "" || cfg.AnthropicAPIKey != "" || cfg.OpenAIAPIKey != "" || cfg.GeminiAPIKey != "" {
		t.Fatal("resolveRuntimeSecrets() mutated loader-owned Config")
	}
}

func TestResolveRuntimeSecretsLoadsStoreAndCorrectsProvider(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	store := testutil.NewMemorySecretStore(map[secretstore.Key]string{
		secretstore.GitHubToken:  " stored-github ",
		secretstore.OpenAIAPIKey: " stored-openai ",
	})
	cfg := &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"}

	got, warnings, err := resolveRuntimeSecrets(cfg, store)
	if err != nil {
		t.Fatalf("resolveRuntimeSecrets() error = %v", err)
	}
	if got.GithubToken != "stored-github" || got.AnthropicAPIKey != "" || got.OpenAIAPIKey != "stored-openai" {
		t.Fatal("resolveRuntimeSecrets() did not resolve stored secrets")
	}
	if got.DefaultProvider != "openai" {
		t.Fatalf("DefaultProvider = %q, want openai", got.DefaultProvider)
	}
	if got := operationKeys(store.Calls, "Get"); len(got) != 4 {
		t.Fatalf("Store.Get() calls = %v, want all four keys", got)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
}

func TestResolveRuntimeSecretsBackendFailuresAreSafeWarnings(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "env-anthropic")
	t.Setenv("OPENAI_API_KEY", "")
	const sensitive = "backend-sensitive-value"
	store := testutil.NewMemorySecretStore(nil)
	store.GetErrors = map[secretstore.Key]error{
		secretstore.GitHubToken:  errors.New("failure: " + sensitive),
		secretstore.OpenAIAPIKey: errors.New("failure: " + sensitive),
	}

	got, warnings, err := resolveRuntimeSecrets(&core.Config{DefaultProvider: "openai"}, store)
	if err != nil {
		t.Fatalf("resolveRuntimeSecrets() error = %v", err)
	}
	if got.DefaultProvider != "claude" || got.AnthropicAPIKey != "env-anthropic" {
		t.Fatalf("resolved config = %#v", got)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings count = %d, want 2", len(warnings))
	}
	for _, warning := range warnings {
		if contains := strings.Contains(warning, sensitive); contains {
			t.Fatal("warning contains backend error or secret")
		}
	}
	if got := operationKeys(store.Calls, "Get"); len(got) != 3 {
		t.Fatalf("Store.Get() calls = %v, want GitHub, OpenAI, and Gemini", got)
	}
}

func TestGHCLITokenReturnsTokenFromGHAuthToken(t *testing.T) {
	dir := writeFakeGH(t, "#!/bin/sh\necho 'gh-token'\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	token, err := ghCLIToken()
	if err != nil {
		t.Fatalf("ghCLIToken() error = %v", err)
	}
	if token != "gh-token" {
		t.Fatalf("ghCLIToken() = %q, want gh-token", token)
	}
}

func TestGHCLITokenTrimsOutput(t *testing.T) {
	dir := writeFakeGH(t, "#!/bin/sh\necho '  gh-token  '\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	token, err := ghCLIToken()
	if err != nil {
		t.Fatalf("ghCLIToken() error = %v", err)
	}
	if token != "gh-token" {
		t.Fatalf("ghCLIToken() = %q, want trimmed gh-token", token)
	}
}

func TestGHCLITokenFailureReturnsError(t *testing.T) {
	dir := writeFakeGH(t, "#!/bin/sh\nexit 1\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, err := ghCLIToken()
	if err == nil {
		t.Fatal("ghCLIToken() error = nil, want exit failure")
	}
}

func writeFakeGH(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestResolveRuntimeSecretsRejectsMissingAISecrets(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	store := testutil.NewMemorySecretStore(nil)

	_, _, err := resolveRuntimeSecrets(&core.Config{}, store)
	if err == nil || err.Error() != "ANTHROPIC_API_KEY、OPENAI_API_KEY、GEMINI_API_KEY のいずれかを設定してください" {
		t.Fatalf("resolveRuntimeSecrets() error = %v", err)
	}
}

func TestMigrateLegacySecretsNoLegacyIsNoop(t *testing.T) {
	store := testutil.NewMemorySecretStore(nil)
	saveCalls := 0

	err := migrateLegacySecrets(&core.Config{DefaultProvider: "claude"}, core.LegacySecrets{}, store, func(*core.Config) error {
		saveCalls++
		return nil
	})
	if err != nil {
		t.Fatalf("migrateLegacySecrets() error = %v", err)
	}
	if len(store.Calls) != 0 || saveCalls != 0 {
		t.Fatal("no-op migration accessed Store or saved Config")
	}
}

func TestMigrateLegacySecretsSetsOnlyMissingKeysThenSavesConfig(t *testing.T) {
	store := testutil.NewMemorySecretStore(map[secretstore.Key]string{
		secretstore.AnthropicAPIKey: "keychain-anthropic",
	})
	cfg := &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"}
	legacy := core.LegacySecrets{
		GithubToken:     " legacy-github ",
		AnthropicAPIKey: "legacy-must-not-overwrite",
		OpenAIAPIKey:    "   ",
	}
	saveCalls := 0

	err := migrateLegacySecrets(cfg, legacy, store, func(got *core.Config) error {
		saveCalls++
		if got.DefaultProvider != cfg.DefaultProvider || got.DefaultLanguage != cfg.DefaultLanguage || got.GithubToken != "" || got.AnthropicAPIKey != "" || got.OpenAIAPIKey != "" {
			t.Fatal("migration attempted to save secrets in runtime Config")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("migrateLegacySecrets() error = %v", err)
	}
	state := store.Snapshot()
	if state[secretstore.GitHubToken] != "legacy-github" {
		t.Fatal("missing legacy secret was not migrated")
	}
	if state[secretstore.AnthropicAPIKey] != "keychain-anthropic" {
		t.Fatal("existing Keychain secret was overwritten")
	}
	setCalls := operationKeys(store.Calls, "Set")
	if len(setCalls) != 1 || setCalls[0] != secretstore.GitHubToken || saveCalls != 1 {
		t.Fatalf("Set calls = %v, save calls = %d", setCalls, saveCalls)
	}
}

func TestMigrateLegacySecretsSetFailureRollsBackCreatedKeysWithoutSaving(t *testing.T) {
	store := testutil.NewMemorySecretStore(nil)
	store.FailSetAt = 2
	legacy := core.LegacySecrets{GithubToken: "legacy-github", AnthropicAPIKey: "legacy-anthropic"}
	saveCalls := 0

	err := migrateLegacySecrets(&core.Config{}, legacy, store, func(*core.Config) error {
		saveCalls++
		return nil
	})
	if err == nil {
		t.Fatal("migrateLegacySecrets() error = nil, want Set failure")
	}
	if state := store.Snapshot(); len(state) != 0 || saveCalls != 0 {
		t.Fatalf("migration failure changed state: values=%#v saveCalls=%d", state, saveCalls)
	}
}

func TestMigrateLegacySecretsConfigFailureRollsBackOnlyCreatedKeys(t *testing.T) {
	store := testutil.NewMemorySecretStore(map[secretstore.Key]string{
		secretstore.GitHubToken: "existing-github",
	})
	legacy := core.LegacySecrets{GithubToken: "legacy-github", AnthropicAPIKey: "legacy-anthropic"}

	err := migrateLegacySecrets(&core.Config{}, legacy, store, func(*core.Config) error {
		return errors.New("config save failed")
	})
	if err == nil {
		t.Fatal("migrateLegacySecrets() error = nil, want config failure")
	}
	state := store.Snapshot()
	if state[secretstore.GitHubToken] != "existing-github" {
		t.Fatal("migration rollback changed a pre-existing key")
	}
	if _, exists := state[secretstore.AnthropicAPIKey]; exists {
		t.Fatal("migration rollback retained a newly-created key")
	}
}

func TestMigrateLegacySecretsIsIdempotent(t *testing.T) {
	store := testutil.NewMemorySecretStore(nil)
	legacy := core.LegacySecrets{OpenAIAPIKey: "legacy-openai"}
	saveCalls := 0
	save := func(*core.Config) error { saveCalls++; return nil }

	if err := migrateLegacySecrets(&core.Config{}, legacy, store, save); err != nil {
		t.Fatalf("first migrateLegacySecrets() error = %v", err)
	}
	if err := migrateLegacySecrets(&core.Config{}, legacy, store, save); err != nil {
		t.Fatalf("second migrateLegacySecrets() error = %v", err)
	}
	setCalls := operationKeys(store.Calls, "Set")
	if state := store.Snapshot(); state[secretstore.OpenAIAPIKey] != "legacy-openai" || len(setCalls) != 1 || saveCalls != 2 {
		t.Fatalf("idempotent state = %#v, sets=%v, saves=%d", state, setCalls, saveCalls)
	}
}

func TestMigrateLegacySecretsRollbackFailureReturnsSafeGuidance(t *testing.T) {
	const sensitive = "legacy-sensitive-value"
	store := testutil.NewMemorySecretStore(nil)
	store.FailDeleteAt = 1
	err := migrateLegacySecrets(&core.Config{}, core.LegacySecrets{GithubToken: sensitive}, store, func(*core.Config) error {
		return errors.New("save failed: " + sensitive)
	})
	if err == nil || !strings.Contains(err.Error(), "復元に失敗") {
		t.Fatalf("migrateLegacySecrets() error = %v, want recovery guidance", err)
	}
	if strings.Contains(err.Error(), sensitive) {
		t.Fatal("migration error contains legacy secret")
	}
}
