package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/issy20/reporepo/internal/core"
	"github.com/issy20/reporepo/internal/secretstore"
)

type fakeSecretStore struct {
	values   map[secretstore.Key]string
	getErrs  map[secretstore.Key]error
	getCalls []secretstore.Key
	setCalls map[secretstore.Key]string
	deletes  []secretstore.Key
}

func (s *fakeSecretStore) Get(key secretstore.Key) (string, error) {
	s.getCalls = append(s.getCalls, key)
	if err := s.getErrs[key]; err != nil {
		return "", err
	}
	value, ok := s.values[key]
	if !ok {
		return "", secretstore.ErrNotFound
	}
	return value, nil
}

func (s *fakeSecretStore) Set(key secretstore.Key, value string) error {
	if s.setCalls == nil {
		s.setCalls = make(map[secretstore.Key]string)
	}
	s.setCalls[key] = value
	return nil
}
func (s *fakeSecretStore) Delete(key secretstore.Key) error {
	s.deletes = append(s.deletes, key)
	return nil
}

func TestResolveRuntimeSecretsEnvironmentTakesPriorityWithoutStoreGet(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", " env-github ")
	t.Setenv("ANTHROPIC_API_KEY", " env-anthropic ")
	t.Setenv("OPENAI_API_KEY", " env-openai ")
	store := &fakeSecretStore{values: map[secretstore.Key]string{
		secretstore.GitHubToken:     "stored-github",
		secretstore.AnthropicAPIKey: "stored-anthropic",
		secretstore.OpenAIAPIKey:    "stored-openai",
	}}
	cfg := &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"}

	got, warnings, err := resolveRuntimeSecrets(cfg, store)
	if err != nil {
		t.Fatalf("resolveRuntimeSecrets() error = %v", err)
	}
	if got.GithubToken != "env-github" || got.AnthropicAPIKey != "env-anthropic" || got.OpenAIAPIKey != "env-openai" {
		t.Fatal("resolveRuntimeSecrets() did not use environment secrets")
	}
	if len(store.getCalls) != 0 {
		t.Fatalf("Store.Get() calls = %v, want none", store.getCalls)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if cfg.GithubToken != "" || cfg.AnthropicAPIKey != "" || cfg.OpenAIAPIKey != "" {
		t.Fatal("resolveRuntimeSecrets() mutated loader-owned Config")
	}
}

func TestResolveRuntimeSecretsLoadsStoreAndCorrectsProvider(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	store := &fakeSecretStore{values: map[secretstore.Key]string{
		secretstore.GitHubToken:  " stored-github ",
		secretstore.OpenAIAPIKey: " stored-openai ",
	}}
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
	if len(store.getCalls) != 3 {
		t.Fatalf("Store.Get() calls = %v, want all three keys", store.getCalls)
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
	store := &fakeSecretStore{getErrs: map[secretstore.Key]error{
		secretstore.GitHubToken:  errors.New("failure: " + sensitive),
		secretstore.OpenAIAPIKey: errors.New("failure: " + sensitive),
	}}

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
	if len(store.getCalls) != 2 {
		t.Fatalf("Store.Get() calls = %v, want GitHub and OpenAI only", store.getCalls)
	}
}

func TestResolveRuntimeSecretsRejectsMissingAISecrets(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	store := &fakeSecretStore{}

	_, _, err := resolveRuntimeSecrets(&core.Config{}, store)
	if err == nil || err.Error() != "ANTHROPIC_API_KEY または OPENAI_API_KEY を設定してください" {
		t.Fatalf("resolveRuntimeSecrets() error = %v", err)
	}
}

func TestMigrateLegacySecretsNoLegacyIsNoop(t *testing.T) {
	store := &fakeSecretStore{}
	saveCalls := 0

	err := migrateLegacySecrets(&core.Config{DefaultProvider: "claude"}, core.LegacySecrets{}, store, func(*core.Config) error {
		saveCalls++
		return nil
	})
	if err != nil {
		t.Fatalf("migrateLegacySecrets() error = %v", err)
	}
	if len(store.getCalls) != 0 || len(store.setCalls) != 0 || len(store.deletes) != 0 || saveCalls != 0 {
		t.Fatal("no-op migration accessed Store or saved Config")
	}
}

func TestMigrateLegacySecretsSetsOnlyMissingKeysThenSavesConfig(t *testing.T) {
	store := &transactionSecretStore{values: map[secretstore.Key]string{
		secretstore.AnthropicAPIKey: "keychain-anthropic",
	}}
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
	if store.values[secretstore.GitHubToken] != "legacy-github" {
		t.Fatal("missing legacy secret was not migrated")
	}
	if store.values[secretstore.AnthropicAPIKey] != "keychain-anthropic" {
		t.Fatal("existing Keychain secret was overwritten")
	}
	if len(store.setCalls) != 1 || store.setCalls[0] != secretstore.GitHubToken || saveCalls != 1 {
		t.Fatalf("Set calls = %v, save calls = %d", store.setCalls, saveCalls)
	}
}

func TestMigrateLegacySecretsSetFailureRollsBackCreatedKeysWithoutSaving(t *testing.T) {
	store := &transactionSecretStore{values: map[secretstore.Key]string{}, failSetAt: 2}
	legacy := core.LegacySecrets{GithubToken: "legacy-github", AnthropicAPIKey: "legacy-anthropic"}
	saveCalls := 0

	err := migrateLegacySecrets(&core.Config{}, legacy, store, func(*core.Config) error {
		saveCalls++
		return nil
	})
	if err == nil {
		t.Fatal("migrateLegacySecrets() error = nil, want Set failure")
	}
	if len(store.values) != 0 || saveCalls != 0 {
		t.Fatalf("migration failure changed state: values=%#v saveCalls=%d", store.values, saveCalls)
	}
}

func TestMigrateLegacySecretsConfigFailureRollsBackOnlyCreatedKeys(t *testing.T) {
	store := &transactionSecretStore{values: map[secretstore.Key]string{
		secretstore.GitHubToken: "existing-github",
	}}
	legacy := core.LegacySecrets{GithubToken: "legacy-github", AnthropicAPIKey: "legacy-anthropic"}

	err := migrateLegacySecrets(&core.Config{}, legacy, store, func(*core.Config) error {
		return errors.New("config save failed")
	})
	if err == nil {
		t.Fatal("migrateLegacySecrets() error = nil, want config failure")
	}
	if store.values[secretstore.GitHubToken] != "existing-github" {
		t.Fatal("migration rollback changed a pre-existing key")
	}
	if _, exists := store.values[secretstore.AnthropicAPIKey]; exists {
		t.Fatal("migration rollback retained a newly-created key")
	}
}

func TestMigrateLegacySecretsIsIdempotent(t *testing.T) {
	store := &transactionSecretStore{values: map[secretstore.Key]string{}}
	legacy := core.LegacySecrets{OpenAIAPIKey: "legacy-openai"}
	saveCalls := 0
	save := func(*core.Config) error { saveCalls++; return nil }

	if err := migrateLegacySecrets(&core.Config{}, legacy, store, save); err != nil {
		t.Fatalf("first migrateLegacySecrets() error = %v", err)
	}
	if err := migrateLegacySecrets(&core.Config{}, legacy, store, save); err != nil {
		t.Fatalf("second migrateLegacySecrets() error = %v", err)
	}
	if store.values[secretstore.OpenAIAPIKey] != "legacy-openai" || len(store.setCalls) != 1 || saveCalls != 2 {
		t.Fatalf("idempotent state = %#v, sets=%v, saves=%d", store.values, store.setCalls, saveCalls)
	}
}

func TestMigrateLegacySecretsRollbackFailureReturnsSafeGuidance(t *testing.T) {
	const sensitive = "legacy-sensitive-value"
	store := &transactionSecretStore{
		values:       map[secretstore.Key]string{},
		failDeleteAt: 1,
	}
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
