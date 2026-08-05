package cmd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/issy20/reporepo/internal/core"
	"github.com/issy20/reporepo/internal/presentation"
	"github.com/issy20/reporepo/internal/secretstore"
	"github.com/issy20/reporepo/internal/testutil"
)

func TestPromptSecretEmptyInputReturnsKeepAction(t *testing.T) {
	edit, err := promptSecretEdit(newConsoleWizardIO(strings.NewReader("\n"), io.Discard, nil), "API key", true)
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
			got, err := promptSecretEdit(newConsoleWizardIO(strings.NewReader(tt.input), io.Discard, nil), "API key", true)
			if err != nil {
				t.Fatalf("promptSecretEdit() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("promptSecretEdit() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestConsoleWizardIOReadLinePromptDecorated(t *testing.T) {
	var out bytes.Buffer
	renderer := presentation.NewRenderer(&out, presentation.Capabilities{Decorated: true, Width: 80})
	wio := newConsoleWizardIO(strings.NewReader("answer\n"), &out, renderer.Prompt)
	if _, err := wio.ReadLine("API key: "); err != nil {
		t.Fatalf("ReadLine() error = %v", err)
	}
	if !strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("decorated prompt has no ANSI: %q", out.String())
	}
	if !strings.Contains(out.String(), "API key: ") {
		t.Fatalf("decorated prompt lost label: %q", out.String())
	}
}

func TestConsoleWizardIOReadSecretPromptDecorated(t *testing.T) {
	var out bytes.Buffer
	renderer := presentation.NewRenderer(&out, presentation.Capabilities{Decorated: true, Width: 80})
	wio := newConsoleWizardIO(strings.NewReader("secret\n"), &out, renderer.Prompt)
	if _, err := wio.ReadSecret("API key: "); err != nil {
		t.Fatalf("ReadSecret() error = %v", err)
	}
	if !strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("decorated secret prompt has no ANSI: %q", out.String())
	}
	if !strings.Contains(out.String(), "API key: ") {
		t.Fatalf("decorated secret prompt lost label: %q", out.String())
	}
}

func TestConsoleWizardIOPlainPromptHasNoANSI(t *testing.T) {
	var out bytes.Buffer
	renderer := presentation.NewRenderer(&out, presentation.Capabilities{Width: 80})
	wio := newConsoleWizardIO(strings.NewReader("answer\n"), &out, renderer.Prompt)
	if _, err := wio.ReadLine("API key: "); err != nil {
		t.Fatalf("ReadLine() error = %v", err)
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("plain prompt contains ANSI: %q", out.String())
	}
	if !strings.Contains(out.String(), "API key: ") {
		t.Fatalf("plain prompt lost label: %q", out.String())
	}
}

func TestConsoleWizardIOReadLinePromptErrorIsReturned(t *testing.T) {
	wio := newConsoleWizardIO(strings.NewReader("answer\n"), io.Discard, func(string) error { return errors.New("prompt failed") })
	if _, err := wio.ReadLine("API key: "); err == nil || !strings.Contains(err.Error(), "prompt failed") {
		t.Fatalf("ReadLine() error = %v, want prompt failure", err)
	}
}

func TestConsoleWizardIOReadSecretPromptErrorIsReturned(t *testing.T) {
	wio := newConsoleWizardIO(strings.NewReader("secret\n"), io.Discard, func(string) error { return errors.New("prompt failed") })
	if _, err := wio.ReadSecret("API key: "); err == nil || !strings.Contains(err.Error(), "prompt failed") {
		t.Fatalf("ReadSecret() error = %v, want prompt failure", err)
	}
}

func TestConsoleWizardIOReadSecretNonTTYReadsLine(t *testing.T) {
	var out bytes.Buffer
	renderer := presentation.NewRenderer(&out, presentation.Capabilities{Width: 80})
	wio := newConsoleWizardIO(strings.NewReader(" plain-secret \n"), &out, renderer.Prompt)
	value, err := wio.ReadSecret("API key: ")
	if err != nil {
		t.Fatalf("ReadSecret() error = %v", err)
	}
	if value != "plain-secret" {
		t.Fatalf("ReadSecret() = %q, want %q", value, "plain-secret")
	}
}

func TestConsoleWizardIOReadSecretEchoSuppressedOnTTY(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	defer r.Close()
	defer w.Close()
	var out bytes.Buffer
	renderer := presentation.NewRenderer(&out, presentation.Capabilities{Width: 80})
	wio := newConsoleWizardIO(r, &out, renderer.Prompt)
	wio.isTerminal = func(int) bool { return true }
	var passwordRead bool
	wio.readPassword = func(int) ([]byte, error) { passwordRead = true; return []byte("p@ss\n"), nil }
	value, err := wio.ReadSecret("API key: ")
	if err != nil {
		t.Fatalf("ReadSecret() error = %v", err)
	}
	if !passwordRead {
		t.Fatal("ReadSecret() did not use ReadPassword on TTY")
	}
	if value != "p@ss" {
		t.Fatalf("ReadSecret() = %q, want %q", value, "p@ss")
	}
	if !strings.Contains(out.String(), "API key: ") {
		t.Fatalf("TTY prompt lost label: %q", out.String())
	}
}

func TestSaveWizardChangesRestoresDeleteAfterLaterFailure(t *testing.T) {
	store := testutil.NewMemorySecretStore(map[secretstore.Key]string{
		secretstore.GitHubToken:     "old-github",
		secretstore.AnthropicAPIKey: "old-anthropic",
	})
	store.FailSetAt = 1
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
	if store.Snapshot()[secretstore.GitHubToken] != "old-github" {
		t.Fatal("deleted secret was not restored")
	}
}

func TestSaveWizardChangesConfigFailureRollsBackInReverseOrder(t *testing.T) {
	store := testutil.NewMemorySecretStore(map[secretstore.Key]string{
		secretstore.AnthropicAPIKey: "old-anthropic",
	})
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
	state := store.Snapshot()
	if _, exists := state[secretstore.GitHubToken]; exists || state[secretstore.AnthropicAPIKey] != "old-anthropic" {
		t.Fatalf("store values after rollback = %#v", state)
	}
	want := []testutil.SecretOperation{
		{Method: "Set", Key: secretstore.GitHubToken}, {Method: "Delete", Key: secretstore.AnthropicAPIKey},
		{Method: "Set", Key: secretstore.AnthropicAPIKey}, {Method: "Delete", Key: secretstore.GitHubToken},
	}
	if !slices.Equal(store.Calls, want) {
		t.Fatalf("operation order = %v, want %v", store.Calls, want)
	}
}

func TestSaveWizardChangesReportsRollbackFailureWithoutLeakingSecret(t *testing.T) {
	const sensitive = "sensitive-old-value"
	store := testutil.NewMemorySecretStore(map[secretstore.Key]string{secretstore.GitHubToken: sensitive})
	store.FailSetAt = 2
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
	store := testutil.NewMemorySecretStore(map[secretstore.Key]string{
		secretstore.GitHubToken:     "old-github",
		secretstore.AnthropicAPIKey: "old-anthropic",
	})
	store.FailSetAt = 2
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
	state := store.Snapshot()
	if state[secretstore.GitHubToken] != "old-github" || state[secretstore.AnthropicAPIKey] != "old-anthropic" {
		t.Fatalf("store values after rollback = %#v", state)
	}
	if saveCalls != 0 {
		t.Fatalf("config save calls = %d, want 0", saveCalls)
	}
	wantOrder := []secretstore.Key{secretstore.GitHubToken, secretstore.AnthropicAPIKey, secretstore.GitHubToken}
	setCalls := operationKeys(store.Calls, "Set")
	if !slices.Equal(setCalls, wantOrder) {
		t.Fatalf("Set order = %v, want %v", setCalls, wantOrder)
	}
}

func TestRunConfigWizard_NoDoesNotSave(t *testing.T) {
	saves := 0
	secretStore := testutil.NewMemorySecretStore(map[secretstore.Key]string{secretstore.AnthropicAPIKey: "stored-key"})
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
	if len(operationKeys(secretStore.Calls, "Set")) != 0 || len(operationKeys(secretStore.Calls, "Delete")) != 0 {
		t.Fatal("cancel changed the Keychain store")
	}
}

func TestRunConfigWizardStoreFailureDoesNotPrompt(t *testing.T) {
	const sensitive = "backend-sensitive-value"
	out := &bytes.Buffer{}
	secretStore := testutil.NewMemorySecretStore(nil)
	secretStore.GetErrors = map[secretstore.Key]error{
		secretstore.GitHubToken: errors.New("failure: " + sensitive),
	}
	err := runConfigWizard(strings.NewReader("should-not-be-read\n"), out,
		func() (*core.Config, error) { return &core.Config{}, nil },
		func(*core.Config) error { t.Fatal("save called"); return nil },
		secretStore,
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
	t.Setenv("GEMINI_API_KEY", "")
	original := &core.Config{GithubToken: "old-token", AnthropicAPIKey: "old-key", OpenAIAPIKey: "remove-me", DefaultProvider: "claude", DefaultLanguage: "ja"}
	secretStore := testutil.NewMemorySecretStore(map[secretstore.Key]string{
		secretstore.GitHubToken: "old-token", secretstore.AnthropicAPIKey: "old-key", secretstore.OpenAIAPIKey: "remove-me",
	})
	var saved *core.Config
	out := &bytes.Buffer{}
	err := runConfigWizard(strings.NewReader(" new-token \n new-key \n-\n\nclaude\nen\nyes\n"), out,
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
	state := secretStore.Snapshot()
	deleteCalls := operationKeys(secretStore.Calls, "Delete")
	if state[secretstore.GitHubToken] != "new-token" || state[secretstore.AnthropicAPIKey] != "new-key" || len(deleteCalls) != 1 || deleteCalls[0] != secretstore.OpenAIAPIKey {
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
	t.Setenv("GEMINI_API_KEY", "")
	var saved *core.Config
	out := &bytes.Buffer{}
	err := runConfigWizard(strings.NewReader("\n\n\n\n\n\nY\n"), out,
		func() (*core.Config, error) {
			return &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"}, nil
		},
		func(cfg *core.Config) error { copy := *cfg; saved = &copy; return nil },
		testutil.NewMemorySecretStore(nil),
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
	t.Setenv("GEMINI_API_KEY", "")
	var saved *core.Config
	out := &bytes.Buffer{}
	err := runConfigWizard(strings.NewReader("\nkey\n\n\ninvalid\nclaude\nfr\nja\ny\n"), out,
		func() (*core.Config, error) {
			return &core.Config{DefaultProvider: "broken", DefaultLanguage: "broken"}, nil
		},
		func(cfg *core.Config) error { saved = cfg; return nil },
		testutil.NewMemorySecretStore(nil),
	)
	if err != nil {
		t.Fatalf("runConfigWizard() error = %v", err)
	}
	if saved == nil || saved.DefaultProvider != "claude" || saved.DefaultLanguage != "ja" {
		t.Fatalf("saved config = %#v", saved)
	}
	if !strings.Contains(out.String(), "claude, openai, gemini") || !strings.Contains(out.String(), "ja, en") {
		t.Fatalf("validation output = %s", out.String())
	}
}

func TestRunConfigWizard_EOFDoesNotSave(t *testing.T) {
	saves := 0
	err := runConfigWizard(strings.NewReader(""), io.Discard,
		func() (*core.Config, error) { return &core.Config{}, nil },
		func(*core.Config) error { saves++; return nil },
		testutil.NewMemorySecretStore(nil),
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
		testutil.NewMemorySecretStore(nil),
	)
	if loadErr == nil || strings.Contains(loadErr.Error(), secret) {
		t.Fatalf("load error = %v", loadErr)
	}

	t.Setenv("ANTHROPIC_API_KEY", "env-key")
	t.Setenv("GEMINI_API_KEY", "")
	saveErr := runConfigWizard(strings.NewReader("\n\n\n\n\n\ny\n"), io.Discard,
		func() (*core.Config, error) {
			return &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"}, nil
		},
		func(*core.Config) error { return errors.New(secret) },
		testutil.NewMemorySecretStore(nil),
	)
	if saveErr == nil || strings.Contains(saveErr.Error(), secret) {
		t.Fatalf("save error = %v", saveErr)
	}
}
