package cmd

import (
	"errors"
	"os"
	"strings"

	"github.com/issy20/reporepo/internal/core"
	"github.com/issy20/reporepo/internal/secretstore"
)

const (
	githubTokenEnv     = "GITHUB_TOKEN"
	anthropicAPIKeyEnv = "ANTHROPIC_API_KEY"
	openAIAPIKeyEnv    = "OPENAI_API_KEY"
)

func resolveRuntimeSecrets(cfg *core.Config, store secretstore.Store) (*core.Config, []string, error) {
	runtimeConfig := *cfg
	warnings := make([]string, 0, 3)

	runtimeConfig.GithubToken = resolveSecret(githubTokenEnv, secretstore.GitHubToken, store, "GitHub token", &warnings)
	runtimeConfig.AnthropicAPIKey = resolveSecret(anthropicAPIKeyEnv, secretstore.AnthropicAPIKey, store, "Anthropic API key", &warnings)
	runtimeConfig.OpenAIAPIKey = resolveSecret(openAIAPIKeyEnv, secretstore.OpenAIAPIKey, store, "OpenAI API key", &warnings)

	hasClaude := runtimeConfig.AnthropicAPIKey != ""
	hasOpenAI := runtimeConfig.OpenAIAPIKey != ""
	if !hasClaude && !hasOpenAI {
		return nil, warnings, errors.New("ANTHROPIC_API_KEY または OPENAI_API_KEY を設定してください")
	}
	if (runtimeConfig.DefaultProvider == "claude" && !hasClaude) ||
		(runtimeConfig.DefaultProvider == "openai" && !hasOpenAI) ||
		(runtimeConfig.DefaultProvider != "claude" && runtimeConfig.DefaultProvider != "openai") {
		if hasClaude {
			runtimeConfig.DefaultProvider = "claude"
		} else {
			runtimeConfig.DefaultProvider = "openai"
		}
	}
	return &runtimeConfig, warnings, nil
}

func resolveSecret(envName string, key secretstore.Key, store secretstore.Store, label string, warnings *[]string) string {
	if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
		return value
	}
	if store == nil {
		*warnings = append(*warnings, label+"をOS資格情報ストアから読み込めませんでした")
		return ""
	}
	value, err := store.Get(key)
	if errors.Is(err, secretstore.ErrNotFound) {
		return ""
	}
	if err != nil {
		*warnings = append(*warnings, label+"をOS資格情報ストアから読み込めませんでした")
		return ""
	}
	return strings.TrimSpace(value)
}

func migrateLegacySecrets(cfg *core.Config, legacy core.LegacySecrets, store secretstore.Store, saveConfig func(*core.Config) error) error {
	entries := legacySecretEntries(legacy)
	if len(entries) == 0 {
		return nil
	}
	if cfg == nil || store == nil || saveConfig == nil {
		return migrationError(false)
	}
	created := make([]secretstore.Key, 0, len(entries))
	for _, entry := range entries {
		_, err := store.Get(entry.key)
		switch {
		case err == nil:
			continue
		case errors.Is(err, secretstore.ErrNotFound):
			if err := store.Set(entry.key, entry.value); err != nil {
				return migrationError(rollbackMigratedSecrets(store, created))
			}
			created = append(created, entry.key)
		default:
			return migrationError(rollbackMigratedSecrets(store, created))
		}
	}
	for _, entry := range entries {
		if _, err := store.Get(entry.key); err != nil {
			return migrationError(rollbackMigratedSecrets(store, created))
		}
	}
	clean := *cfg
	clean.GithubToken = ""
	clean.AnthropicAPIKey = ""
	clean.OpenAIAPIKey = ""
	if err := saveConfig(&clean); err != nil {
		return migrationError(rollbackMigratedSecrets(store, created))
	}
	return nil
}

func rollbackMigratedSecrets(store secretstore.Store, created []secretstore.Key) bool {
	failed := false
	for i := len(created) - 1; i >= 0; i-- {
		if err := store.Delete(created[i]); err != nil {
			failed = true
		}
	}
	return failed
}

func migrationError(rollbackFailed bool) error {
	return migrationFailure{rollbackFailed: rollbackFailed}
}

type migrationFailure struct {
	rollbackFailed bool
}

func (e migrationFailure) Error() string {
	if e.rollbackFailed {
		return "旧形式のsecret移行後の復元に失敗しました。OS資格情報ストアの設定を確認してください"
	}
	return "旧形式のsecretを移行できませんでした。OS資格情報ストアを確認するか、環境変数で一時設定してください"
}

func isMigrationFailure(err error) bool {
	var target migrationFailure
	return errors.As(err, &target)
}

type legacySecretEntry struct {
	key   secretstore.Key
	value string
}

func legacySecretEntries(legacy core.LegacySecrets) []legacySecretEntry {
	candidates := []legacySecretEntry{
		{key: secretstore.GitHubToken, value: strings.TrimSpace(legacy.GithubToken)},
		{key: secretstore.AnthropicAPIKey, value: strings.TrimSpace(legacy.AnthropicAPIKey)},
		{key: secretstore.OpenAIAPIKey, value: strings.TrimSpace(legacy.OpenAIAPIKey)},
	}
	entries := make([]legacySecretEntry, 0, len(candidates))
	for _, entry := range candidates {
		if entry.value != "" {
			entries = append(entries, entry)
		}
	}
	return entries
}
