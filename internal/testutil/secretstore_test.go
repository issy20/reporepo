package testutil

import (
	"errors"
	"slices"
	"testing"

	"github.com/issy20/reporepo/internal/secretstore"
)

func TestMemorySecretStoreGetMissingReturnsErrNotFound(t *testing.T) {
	store := NewMemorySecretStore(nil)

	_, err := store.Get(secretstore.GitHubToken)
	if !errors.Is(err, secretstore.ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
}

func TestMemorySecretStoreSetGetUpdateAndDelete(t *testing.T) {
	store := NewMemorySecretStore(map[secretstore.Key]string{secretstore.GitHubToken: "initial"})

	if got, err := store.Get(secretstore.GitHubToken); err != nil || got != "initial" {
		t.Fatalf("initial Get() = %q, %v", got, err)
	}
	if err := store.Set(secretstore.AnthropicAPIKey, "created"); err != nil {
		t.Fatalf("Set(create) error = %v", err)
	}
	if got, err := store.Get(secretstore.AnthropicAPIKey); err != nil || got != "created" {
		t.Fatalf("created Get() = %q, %v", got, err)
	}
	if err := store.Set(secretstore.GitHubToken, "updated"); err != nil {
		t.Fatalf("Set(update) error = %v", err)
	}
	if got, _ := store.Get(secretstore.GitHubToken); got != "updated" {
		t.Fatalf("updated Get() = %q", got)
	}
	if err := store.Delete(secretstore.GitHubToken); err != nil {
		t.Fatalf("Delete(existing) error = %v", err)
	}
	if _, err := store.Get(secretstore.GitHubToken); !errors.Is(err, secretstore.ErrNotFound) {
		t.Fatalf("Get(after Delete) error = %v", err)
	}
	if err := store.Delete(secretstore.OpenAIAPIKey); err != nil {
		t.Fatalf("Delete(missing) error = %v", err)
	}
}

func TestMemorySecretStoreClonesInitialValuesAndSnapshots(t *testing.T) {
	initial := map[secretstore.Key]string{secretstore.GitHubToken: "initial"}
	store := NewMemorySecretStore(initial)
	initial[secretstore.GitHubToken] = "changed-outside"

	if got, _ := store.Get(secretstore.GitHubToken); got != "initial" {
		t.Fatalf("constructor retained caller map: Get() = %q", got)
	}
	snapshot := store.Snapshot()
	snapshot[secretstore.GitHubToken] = "changed-snapshot"
	if got, _ := store.Get(secretstore.GitHubToken); got != "initial" {
		t.Fatalf("Snapshot exposed internal map: Get() = %q", got)
	}
}

func TestMemorySecretStoreRecordsOperationsInOrder(t *testing.T) {
	store := NewMemorySecretStore(nil)
	_, _ = store.Get(secretstore.GitHubToken)
	_ = store.Set(secretstore.AnthropicAPIKey, "value")
	_ = store.Delete(secretstore.OpenAIAPIKey)

	want := []SecretOperation{
		{Method: "Get", Key: secretstore.GitHubToken},
		{Method: "Set", Key: secretstore.AnthropicAPIKey},
		{Method: "Delete", Key: secretstore.OpenAIAPIKey},
	}
	if !slices.Equal(store.Calls, want) {
		t.Fatalf("Calls = %#v, want %#v", store.Calls, want)
	}
}

func TestMemorySecretStoreInjectsKeyErrorsWithoutChangingState(t *testing.T) {
	getErr := errors.New("get failed")
	setErr := errors.New("set failed")
	deleteErr := errors.New("delete failed")
	store := NewMemorySecretStore(map[secretstore.Key]string{
		secretstore.GitHubToken:     "github",
		secretstore.AnthropicAPIKey: "anthropic",
	})
	store.GetErrors = map[secretstore.Key]error{secretstore.GitHubToken: getErr}
	store.SetErrors = map[secretstore.Key]error{secretstore.AnthropicAPIKey: setErr}
	store.DeleteErrors = map[secretstore.Key]error{secretstore.GitHubToken: deleteErr}

	if _, err := store.Get(secretstore.GitHubToken); !errors.Is(err, getErr) {
		t.Fatalf("Get() error = %v", err)
	}
	if err := store.Set(secretstore.AnthropicAPIKey, "changed"); !errors.Is(err, setErr) {
		t.Fatalf("Set() error = %v", err)
	}
	if err := store.Delete(secretstore.GitHubToken); !errors.Is(err, deleteErr) {
		t.Fatalf("Delete() error = %v", err)
	}
	if got := store.Snapshot(); got[secretstore.GitHubToken] != "github" || got[secretstore.AnthropicAPIKey] != "anthropic" {
		t.Fatalf("state after failures = %#v", got)
	}
	if len(store.Calls) != 3 {
		t.Fatalf("failed operations were not recorded: %#v", store.Calls)
	}
}

func TestMemorySecretStoreFailsOnlyConfiguredNthMutation(t *testing.T) {
	store := NewMemorySecretStore(nil)
	store.FailSetAt = 2
	store.FailDeleteAt = 2

	if err := store.Set(secretstore.GitHubToken, "github"); err != nil {
		t.Fatalf("first Set() error = %v", err)
	}
	if err := store.Set(secretstore.AnthropicAPIKey, "anthropic"); err == nil {
		t.Fatal("second Set() error = nil")
	}
	if err := store.Set(secretstore.OpenAIAPIKey, "openai"); err != nil {
		t.Fatalf("third Set() error = %v", err)
	}
	if err := store.Delete(secretstore.GitHubToken); err != nil {
		t.Fatalf("first Delete() error = %v", err)
	}
	if err := store.Delete(secretstore.OpenAIAPIKey); err == nil {
		t.Fatal("second Delete() error = nil")
	}
	if _, exists := store.Snapshot()[secretstore.AnthropicAPIKey]; exists {
		t.Fatal("failed Set changed state")
	}
	if got := store.Snapshot()[secretstore.OpenAIAPIKey]; got != "openai" {
		t.Fatal("failed Delete changed state")
	}
}
