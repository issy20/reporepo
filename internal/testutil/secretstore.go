// Package testutil provides shared in-memory test doubles.
package testutil

import (
	"errors"

	"github.com/issy20/reporepo/internal/secretstore"
)

// SecretOperation records one Store method call without retaining its value.
type SecretOperation struct {
	Method string
	Key    secretstore.Key
}

// MemorySecretStore is an in-memory secretstore.Store for tests.
type MemorySecretStore struct {
	Calls        []SecretOperation
	GetErrors    map[secretstore.Key]error
	SetErrors    map[secretstore.Key]error
	DeleteErrors map[secretstore.Key]error
	FailSetAt    int
	FailDeleteAt int

	setCount    int
	deleteCount int
	values      map[secretstore.Key]string
}

var _ secretstore.Store = (*MemorySecretStore)(nil)

// NewMemorySecretStore creates a store with a cloned copy of initial values.
func NewMemorySecretStore(initial map[secretstore.Key]string) *MemorySecretStore {
	values := make(map[secretstore.Key]string, len(initial))
	for key, value := range initial {
		values[key] = value
	}
	return &MemorySecretStore{values: values}
}

func (s *MemorySecretStore) Get(key secretstore.Key) (string, error) {
	s.Calls = append(s.Calls, SecretOperation{Method: "Get", Key: key})
	if err := s.GetErrors[key]; err != nil {
		return "", err
	}
	value, ok := s.values[key]
	if !ok {
		return "", secretstore.ErrNotFound
	}
	return value, nil
}

func (s *MemorySecretStore) Set(key secretstore.Key, value string) error {
	s.Calls = append(s.Calls, SecretOperation{Method: "Set", Key: key})
	s.setCount++
	if err := s.SetErrors[key]; err != nil {
		return err
	}
	if s.FailSetAt > 0 && s.setCount == s.FailSetAt {
		return errors.New("injected Set failure")
	}
	s.values[key] = value
	return nil
}

func (s *MemorySecretStore) Delete(key secretstore.Key) error {
	s.Calls = append(s.Calls, SecretOperation{Method: "Delete", Key: key})
	s.deleteCount++
	if err := s.DeleteErrors[key]; err != nil {
		return err
	}
	if s.FailDeleteAt > 0 && s.deleteCount == s.FailDeleteAt {
		return errors.New("injected Delete failure")
	}
	delete(s.values, key)
	return nil
}

// Snapshot returns a cloned copy of current values.
func (s *MemorySecretStore) Snapshot() map[secretstore.Key]string {
	values := make(map[secretstore.Key]string, len(s.values))
	for key, value := range s.values {
		values[key] = value
	}
	return values
}
