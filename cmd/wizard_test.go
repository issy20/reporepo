package cmd

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/issy20/reporepo/internal/core"
	"github.com/issy20/reporepo/internal/secretstore"
)

func TestPromptSecretEmptyInputReturnsKeepAction(t *testing.T) {
	edit, err := promptSecretEdit(newConsoleWizardIO(strings.NewReader("\n"), io.Discard), "API key", true)
	if err != nil {
		t.Fatalf("promptSecret() error = %v", err)
	}
	if edit.action != keepSecret || edit.value != "" {
		t.Fatalf("promptSecret() edit = %#v, want keep", edit)
	}
}

func TestPromptSecretEditReturnsSetAndDeleteActions(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  secretEdit
	}{
		{name: "set", input: " new-value \n", want: secretEdit{action: setSecret, value: "new-value"}},
		{name: "delete", input: "-\n", want: secretEdit{action: deleteSecret}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := promptSecretEdit(newConsoleWizardIO(strings.NewReader(tt.input), io.Discard), "API key", true)
			if err != nil {
				t.Fatalf("promptSecretEdit() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("promptSecretEdit() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

type transactionSecretStore struct {
	values       map[secretstore.Key]string
	setCalls     []secretstore.Key
	deleteCalls  []secretstore.Key
	operations   []string
	failSetAt    int
	failDeleteAt int
}

func (s *transactionSecretStore) Get(key secretstore.Key) (string, error) {
	value, ok := s.values[key]
	if !ok {
		return "", secretstore.ErrNotFound
	}
	return value, nil
}

func (s *transactionSecretStore) Set(key secretstore.Key, value string) error {
	s.setCalls = append(s.setCalls, key)
	s.operations = append(s.operations, "set:"+string(key))
	if s.failSetAt > 0 && len(s.setCalls) == s.failSetAt {
		return errors.New("set failed")
	}
	s.values[key] = value
	return nil
}

func (s *transactionSecretStore) Delete(key secretstore.Key) error {
	s.deleteCalls = append(s.deleteCalls, key)
	s.operations = append(s.operations, "delete:"+string(key))
	if s.failDeleteAt > 0 && len(s.deleteCalls) == s.failDeleteAt {
		return errors.New("delete failed")
	}
	delete(s.values, key)
	return nil
}

func TestSaveWizardChangesRestoresDeleteAfterLaterFailure(t *testing.T) {
	store := &transactionSecretStore{
		values: map[secretstore.Key]string{
			secretstore.GitHubToken:     "old-github",
			secretstore.AnthropicAPIKey: "old-anthropic",
		},
		failSetAt: 1,
	}
	snapshots := map[secretstore.Key]secretSnapshot{
		secretstore.GitHubToken:     {value: "old-github", exists: true},
		secretstore.AnthropicAPIKey: {value: "old-anthropic", exists: true},
	}
	edits := map[secretstore.Key]secretEdit{
		secretstore.GitHubToken:     {action: deleteSecret},
		secretstore.AnthropicAPIKey: {action: setSecret, value: "new-anthropic"},
	}

	if err := saveWizardChanges(store, snapshots, edits, func() error { return nil }); err == nil {
		t.Fatal("saveWizardChanges() error = nil, want failure")
	}
	if store.values[secretstore.GitHubToken] != "old-github" {
		t.Fatal("deleted secret was not restored")
	}
}

func TestSaveWizardChangesConfigFailureRollsBackInReverseOrder(t *testing.T) {
	store := &transactionSecretStore{values: map[secretstore.Key]string{
		secretstore.AnthropicAPIKey: "old-anthropic",
	}}
	snapshots := map[secretstore.Key]secretSnapshot{
		secretstore.GitHubToken:     {},
		secretstore.AnthropicAPIKey: {value: "old-anthropic", exists: true},
	}
	edits := map[secretstore.Key]secretEdit{
		secretstore.GitHubToken:     {action: setSecret, value: "new-github"},
		secretstore.AnthropicAPIKey: {action: deleteSecret},
	}

	err := saveWizardChanges(store, snapshots, edits, func() error { return errors.New("config failed") })
	if err == nil {
		t.Fatal("saveWizardChanges() error = nil, want config failure")
	}
	if _, exists := store.values[secretstore.GitHubToken]; exists || store.values[secretstore.AnthropicAPIKey] != "old-anthropic" {
		t.Fatalf("store values after rollback = %#v", store.values)
	}
	want := []string{
		"set:github-token", "delete:anthropic-api-key",
		"set:anthropic-api-key", "delete:github-token",
	}
	if !slices.Equal(store.operations, want) {
		t.Fatalf("operation order = %v, want %v", store.operations, want)
	}
}

func TestSaveWizardChangesReportsRollbackFailureWithoutLeakingSecret(t *testing.T) {
	const sensitive = "sensitive-old-value"
	store := &transactionSecretStore{
		values:    map[secretstore.Key]string{secretstore.GitHubToken: sensitive},
		failSetAt: 2,
	}
	snapshots := map[secretstore.Key]secretSnapshot{
		secretstore.GitHubToken: {value: sensitive, exists: true},
	}
	edits := map[secretstore.Key]secretEdit{
		secretstore.GitHubToken: {action: setSecret, value: "new-value"},
	}

	err := saveWizardChanges(store, snapshots, edits, func() error { return errors.New("config failed: " + sensitive) })
	if err == nil || !strings.Contains(err.Error(), "復元に失敗") {
		t.Fatalf("saveWizardChanges() error = %v, want recovery guidance", err)
	}
	if strings.Contains(err.Error(), sensitive) {
		t.Fatal("rollback error contains secret")
	}
}

func TestSaveWizardChangesRollsBackFirstSetWhenSecondSetFails(t *testing.T) {
	store := &transactionSecretStore{
		values: map[secretstore.Key]string{
			secretstore.GitHubToken:     "old-github",
			secretstore.AnthropicAPIKey: "old-anthropic",
		},
		failSetAt: 2,
	}
	snapshots := map[secretstore.Key]secretSnapshot{
		secretstore.GitHubToken:     {value: "old-github", exists: true},
		secretstore.AnthropicAPIKey: {value: "old-anthropic", exists: true},
	}
	edits := map[secretstore.Key]secretEdit{
		secretstore.GitHubToken:     {action: setSecret, value: "new-github"},
		secretstore.AnthropicAPIKey: {action: setSecret, value: "new-anthropic"},
	}
	saveCalls := 0

	err := saveWizardChanges(store, snapshots, edits, func() error {
		saveCalls++
		return nil
	})
	if err == nil {
		t.Fatal("saveWizardChanges() error = nil, want Set failure")
	}
	if store.values[secretstore.GitHubToken] != "old-github" || store.values[secretstore.AnthropicAPIKey] != "old-anthropic" {
		t.Fatalf("store values after rollback = %#v", store.values)
	}
	if saveCalls != 0 {
		t.Fatalf("config save calls = %d, want 0", saveCalls)
	}
	wantOrder := []secretstore.Key{secretstore.GitHubToken, secretstore.AnthropicAPIKey, secretstore.GitHubToken}
	if !slices.Equal(store.setCalls, wantOrder) {
		t.Fatalf("Set order = %v, want %v", store.setCalls, wantOrder)
	}
}

func TestRunConfigWizard_NoDoesNotSave(t *testing.T) {
	saves := 0
	secretStore := &fakeSecretStore{values: map[secretstore.Key]string{secretstore.AnthropicAPIKey: "stored-key"}}
	in := strings.NewReader("\n\n\n\n\nno\n")
	out := &bytes.Buffer{}
	err := runConfigWizard(in, out,
		func() (*core.Config, error) {
			return &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"}, nil
		},
		func(*core.Config) error { saves++; return nil },
		secretStore,
	)
	if err != nil {
		t.Fatalf("runConfigWizard() error = %v", err)
	}
	if saves != 0 {
		t.Fatalf("save calls = %d, want 0", saves)
	}
	if len(secretStore.setCalls) != 0 || len(secretStore.deletes) != 0 {
		t.Fatal("cancel changed the Keychain store")
	}
}

func TestRunConfigWizardStoreFailureDoesNotPrompt(t *testing.T) {
	const sensitive = "backend-sensitive-value"
	out := &bytes.Buffer{}
	err := runConfigWizard(strings.NewReader("should-not-be-read\n"), out,
		func() (*core.Config, error) { return &core.Config{}, nil },
		func(*core.Config) error { t.Fatal("save called"); return nil },
		&fakeSecretStore{getErrs: map[secretstore.Key]error{
			secretstore.GitHubToken: errors.New("failure: " + sensitive),
		}},
	)
	if err == nil {
		t.Fatal("runConfigWizard() error = nil, want store error")
	}
	if strings.Contains(err.Error(), sensitive) || out.Len() != 0 {
		t.Fatal("store failure leaked details or started prompting")
	}
}

func TestRunConfigWizard_UpdatesAndSavesAfterYes(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	original := &core.Config{GithubToken: "old-token", AnthropicAPIKey: "old-key", OpenAIAPIKey: "remove-me", DefaultProvider: "claude", DefaultLanguage: "ja"}
	secretStore := &fakeSecretStore{values: map[secretstore.Key]string{
		secretstore.GitHubToken: "old-token", secretstore.AnthropicAPIKey: "old-key", secretstore.OpenAIAPIKey: "remove-me",
	}}
	var saved *core.Config
	out := &bytes.Buffer{}
	err := runConfigWizard(strings.NewReader(" new-token \n new-key \n-\nclaude\nen\nyes\n"), out,
		func() (*core.Config, error) { return original, nil },
		func(cfg *core.Config) error { copy := *cfg; saved = &copy; return nil },
		secretStore,
	)
	if err != nil {
		t.Fatalf("runConfigWizard() error = %v", err)
	}
	if saved == nil {
		t.Fatal("configuration was not saved")
	}
	if saved.GithubToken != "" || saved.AnthropicAPIKey != "" || saved.OpenAIAPIKey != "" || saved.DefaultProvider != "claude" || saved.DefaultLanguage != "en" {
		t.Fatalf("saved config = %#v", saved)
	}
	if secretStore.setCalls[secretstore.GitHubToken] != "new-token" || secretStore.setCalls[secretstore.AnthropicAPIKey] != "new-key" || len(secretStore.deletes) != 1 || secretStore.deletes[0] != secretstore.OpenAIAPIKey {
		t.Fatal("secret edits were not applied to the Keychain store")
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
		&fakeSecretStore{},
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
		&fakeSecretStore{},
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
		&fakeSecretStore{},
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
		&fakeSecretStore{},
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
		&fakeSecretStore{},
	)
	if saveErr == nil || strings.Contains(saveErr.Error(), secret) {
		t.Fatalf("save error = %v", saveErr)
	}
}
