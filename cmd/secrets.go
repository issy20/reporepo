package cmd

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/issy20/reporepo/internal/core"
	"github.com/issy20/reporepo/internal/secretstore"
)

const (
	githubTokenEnv     = "GITHUB_TOKEN"
	anthropicAPIKeyEnv = "ANTHROPIC_API_KEY"
	openAIAPIKeyEnv    = "OPENAI_API_KEY"
	geminiAPIKeyEnv    = "GEMINI_API_KEY"
)

// resolveRuntimeSecrets は環境変数・OS資格情報ストアから secret を解決する。requireAI が false のときは
// AI キー未設定エラーと provider フォールバックをスキップする（GitHub token のみで動作するコマンド用）。
func resolveRuntimeSecrets(cfg *core.Config, store secretstore.Store, requireAI bool) (*core.Config, []string, error) {
	runtimeConfig := *cfg
	warnings := make([]string, 0, 4)

	runtimeConfig.GithubToken = resolveSecret(githubTokenEnv, secretstore.GitHubToken, store, "GitHub token", &warnings)
	runtimeConfig.AnthropicAPIKey = resolveSecret(anthropicAPIKeyEnv, secretstore.AnthropicAPIKey, store, "Anthropic API key", &warnings)
	runtimeConfig.OpenAIAPIKey = resolveSecret(openAIAPIKeyEnv, secretstore.OpenAIAPIKey, store, "OpenAI API key", &warnings)
	runtimeConfig.GeminiAPIKey = resolveSecret(geminiAPIKeyEnv, secretstore.GeminiAPIKey, store, "Gemini API key", &warnings)

	if !requireAI {
		return &runtimeConfig, warnings, nil
	}

	hasClaude := runtimeConfig.AnthropicAPIKey != ""
	hasOpenAI := runtimeConfig.OpenAIAPIKey != ""
	hasGemini := runtimeConfig.GeminiAPIKey != ""
	if !hasClaude && !hasOpenAI && !hasGemini {
		return nil, warnings, errors.New("ANTHROPIC_API_KEY、OPENAI_API_KEY、GEMINI_API_KEY のいずれかを設定してください")
	}
	if (runtimeConfig.DefaultProvider == "claude" && !hasClaude) ||
		(runtimeConfig.DefaultProvider == "openai" && !hasOpenAI) ||
		(runtimeConfig.DefaultProvider == "gemini" && !hasGemini) ||
		(runtimeConfig.DefaultProvider != "claude" && runtimeConfig.DefaultProvider != "openai" && runtimeConfig.DefaultProvider != "gemini") {
		if hasClaude {
			runtimeConfig.DefaultProvider = "claude"
		} else if hasOpenAI {
			runtimeConfig.DefaultProvider = "openai"
		} else {
			runtimeConfig.DefaultProvider = "gemini"
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

// ghCLIToken は gh コマンドの認証トークンを取得する。失敗時はエラーを返す。
func ghCLIToken() (string, error) {
	cmd := exec.Command("gh", "auth", "token")
	cmd.Stderr = io.Discard
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
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
	clean.GeminiAPIKey = ""
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
		{key: secretstore.GeminiAPIKey, value: strings.TrimSpace(legacy.GeminiAPIKey)},
	}
	entries := make([]legacySecretEntry, 0, len(candidates))
	for _, entry := range candidates {
		if entry.value != "" {
			entries = append(entries, entry)
		}
	}
	return entries
}
