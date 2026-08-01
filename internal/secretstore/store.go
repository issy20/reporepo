package secretstore

import "errors"

type Key string

const (
	GitHubToken     Key = "github-token"
	AnthropicAPIKey Key = "anthropic-api-key"
	OpenAIAPIKey    Key = "openai-api-key"
)

var (
	ErrNotFound    = errors.New("secret not found")
	ErrInvalidKey  = errors.New("invalid secret key")
	ErrEmptySecret = errors.New("secret must not be empty")
	ErrBackend     = errors.New("secret store backend failure")
)

func (k Key) valid() bool {
	switch k {
	case GitHubToken, AnthropicAPIKey, OpenAIAPIKey:
		return true
	default:
		return false
	}
}

type Store interface {
	Get(Key) (string, error)
	Set(Key, string) error
	Delete(Key) error
}
