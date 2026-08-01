package secretstore

import (
	"errors"
	"fmt"

	keyring "github.com/zalando/go-keyring"
)

const serviceName = "reporepo"

type keyringBackend struct {
	get    func(service, account string) (string, error)
	set    func(service, account, secret string) error
	delete func(service, account string) error
}

type keyringStore struct {
	backend  keyringBackend
	notFound error
}

func newKeyringStore(backend keyringBackend, notFound error) Store {
	return &keyringStore{backend: backend, notFound: notFound}
}

// NewKeyringStore は実行OSの資格情報ストアを利用するStoreを返す。
func NewKeyringStore() Store {
	return newKeyringStore(keyringBackend{
		get:    keyring.Get,
		set:    keyring.Set,
		delete: keyring.Delete,
	}, keyring.ErrNotFound)
}

func (s *keyringStore) Get(key Key) (string, error) {
	if !key.valid() {
		return "", ErrInvalidKey
	}
	secret, err := s.backend.get(serviceName, string(key))
	if errors.Is(err, s.notFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get secret: %w", ErrBackend)
	}
	return secret, nil
}

func (s *keyringStore) Set(key Key, secret string) error {
	if !key.valid() {
		return ErrInvalidKey
	}
	if secret == "" {
		return ErrEmptySecret
	}
	if err := s.backend.set(serviceName, string(key), secret); err != nil {
		return fmt.Errorf("set secret: %w", ErrBackend)
	}
	return nil
}

func (s *keyringStore) Delete(key Key) error {
	if !key.valid() {
		return ErrInvalidKey
	}
	err := s.backend.delete(serviceName, string(key))
	if errors.Is(err, s.notFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete secret: %w", ErrBackend)
	}
	return nil
}
