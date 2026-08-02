package secretstore

import (
	"errors"
	"strings"
	"testing"
)

func TestKeyringStoreGetDistinguishesNotFoundFromBackendFailure(t *testing.T) {
	backendNotFound := errors.New("backend: item not found")
	backendFailure := errors.New("backend unavailable")

	tests := []struct {
		name    string
		getErr  error
		wantErr error
	}{
		{name: "not found", getErr: backendNotFound, wantErr: ErrNotFound},
		{name: "backend failure", getErr: backendFailure, wantErr: ErrBackend},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newKeyringStore(keyringBackend{
				get: func(service, account string) (string, error) {
					return "", tt.getErr
				},
			}, backendNotFound)

			_, err := store.Get(GitHubToken)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Get() error = %v, want error matching %v", err, tt.wantErr)
			}
			if tt.getErr == backendFailure && errors.Is(err, ErrNotFound) {
				t.Fatal("Get() converted a backend failure to ErrNotFound")
			}
		})
	}
}

func TestKeyringStoreRejectsInvalidKey(t *testing.T) {
	called := false
	store := newKeyringStore(keyringBackend{
		get: func(service, account string) (string, error) {
			called = true
			return "", nil
		},
	}, errors.New("not found"))

	_, err := store.Get(Key("invalid"))
	if err == nil {
		t.Fatal("Get() error = nil, want invalid key error")
	}
	if called {
		t.Fatal("Get() called backend for an invalid key")
	}
}

func TestKeyringStoreSetRejectsEmptySecret(t *testing.T) {
	called := false
	store := newKeyringStore(keyringBackend{
		set: func(service, account, secret string) error {
			called = true
			return nil
		},
	}, errors.New("not found"))

	err := store.Set(GitHubToken, "")
	if err == nil {
		t.Fatal("Set() error = nil, want empty secret error")
	}
	if called {
		t.Fatal("Set() called backend for an empty secret")
	}
}

func TestKeyringStoreDeleteTreatsNotFoundAsSuccess(t *testing.T) {
	backendNotFound := errors.New("backend: item not found")
	store := newKeyringStore(keyringBackend{
		delete: func(service, account string) error {
			return backendNotFound
		},
	}, backendNotFound)

	if err := store.Delete(OpenAIAPIKey); err != nil {
		t.Fatalf("Delete() error = %v, want nil", err)
	}
}

func TestKeyringStoreDelegatesWithServiceAndAccount(t *testing.T) {
	const secret = "test-secret-value"
	var calls []string
	store := newKeyringStore(keyringBackend{
		get: func(service, account string) (string, error) {
			calls = append(calls, "get:"+service+":"+account)
			return secret, nil
		},
		set: func(service, account, gotSecret string) error {
			calls = append(calls, "set:"+service+":"+account+":"+gotSecret)
			return nil
		},
		delete: func(service, account string) error {
			calls = append(calls, "delete:"+service+":"+account)
			return nil
		},
	}, errors.New("not found"))

	if _, err := store.Get(AnthropicAPIKey); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if err := store.Set(AnthropicAPIKey, secret); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := store.Delete(AnthropicAPIKey); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	want := []string{
		"get:reporepo:anthropic-api-key",
		"set:reporepo:anthropic-api-key:" + secret,
		"delete:reporepo:anthropic-api-key",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("backend calls = %q, want %q", calls, want)
	}
}

func TestKeyringStoreAcceptsGeminiAPIKeyAccount(t *testing.T) {
	var gotService, gotAccount string
	store := newKeyringStore(keyringBackend{
		get: func(service, account string) (string, error) {
			gotService, gotAccount = service, account
			return "gemini-secret", nil
		},
	}, errors.New("not found"))

	secret, err := store.Get(GeminiAPIKey)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if secret != "gemini-secret" || gotService != "reporepo" || gotAccount != "gemini-api-key" {
		t.Fatalf("Get() = %q, service = %q, account = %q", secret, gotService, gotAccount)
	}
}

func TestKeyringStoreErrorsDoNotContainSecret(t *testing.T) {
	const secret = "sensitive-test-value"
	backendErr := errors.New("backend rejected " + secret)
	store := newKeyringStore(keyringBackend{
		set: func(service, account, gotSecret string) error {
			return backendErr
		},
	}, errors.New("not found"))

	err := store.Set(GitHubToken, secret)
	if err == nil {
		t.Fatal("Set() error = nil, want backend error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("Set() error contains the secret value")
	}
}

func TestKeyringStoreSetAndDeleteRejectInvalidKey(t *testing.T) {
	store := newKeyringStore(keyringBackend{
		set: func(service, account, secret string) error {
			t.Fatal("Set() called backend for invalid key")
			return nil
		},
		delete: func(service, account string) error {
			t.Fatal("Delete() called backend for invalid key")
			return nil
		},
	}, errors.New("not found"))

	if err := store.Set(Key("invalid"), "value"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("Set() error = %v, want ErrInvalidKey", err)
	}
	if err := store.Delete(Key("invalid")); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("Delete() error = %v, want ErrInvalidKey", err)
	}
}
