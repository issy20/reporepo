package core

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Config はアプリの設定情報を保持する。
type Config struct {
	GithubToken     string `json:"-"`
	AnthropicAPIKey string `json:"-"`
	OpenAIAPIKey    string `json:"-"`
	DefaultProvider string `json:"default_provider"`
	DefaultLanguage string `json:"default_language"`
}

type configFile struct {
	GithubToken     string `json:"github_token,omitempty"`
	AnthropicAPIKey string `json:"anthropic_api_key,omitempty"`
	OpenAIAPIKey    string `json:"openai_api_key,omitempty"`
	DefaultProvider string `json:"default_provider"`
	DefaultLanguage string `json:"default_language"`
}

// LegacySecrets は旧形式config.jsonから分離した移行対象のsecretを保持する。
type LegacySecrets struct {
	GithubToken     string
	AnthropicAPIKey string
	OpenAIAPIKey    string
}

// configFilePath は設定ファイルのパス。テストで上書き可能。
var configFilePath string

// resolveConfigPath は設定ファイルのパスを解決する。
func resolveConfigPath() (string, error) {
	if configFilePath != "" {
		return configFilePath, nil
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(configDir, "reporepo", "config.json"), nil
}

// ConfigFilePath は実行環境における設定ファイルの保存先を返す。
func ConfigFilePath() (string, error) {
	return resolveConfigPath()
}

// LoadConfigFile は非secret設定と旧形式の移行対象secretを分離して読み込む。
func LoadConfigFile() (*Config, LegacySecrets, error) {
	path, err := resolveConfigPath()
	if err != nil {
		return nil, LegacySecrets{}, err
	}

	cfg := &Config{
		DefaultProvider: "claude",
		DefaultLanguage: "ja",
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, LegacySecrets{}, nil
		}
		return nil, LegacySecrets{}, err
	}

	stored := configFile{
		DefaultProvider: cfg.DefaultProvider,
		DefaultLanguage: cfg.DefaultLanguage,
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, LegacySecrets{}, err
	}

	cfg.DefaultProvider = stored.DefaultProvider
	cfg.DefaultLanguage = stored.DefaultLanguage
	legacy := LegacySecrets{
		GithubToken:     stored.GithubToken,
		AnthropicAPIKey: stored.AnthropicAPIKey,
		OpenAIAPIKey:    stored.OpenAIAPIKey,
	}
	return cfg, legacy, nil
}

// LoadStoredConfig は設定ファイルだけを読み込み、legacy secretを破棄して返す。
func LoadStoredConfig() (*Config, error) {
	cfg, _, err := LoadConfigFile()
	return cfg, err
}

// SaveConfig は設定をファイルに 0600 パーミッションで保存する。
func SaveConfig(cfg *Config) error {
	path, err := resolveConfigPath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	stored := configFile{
		DefaultProvider: cfg.DefaultProvider,
		DefaultLanguage: cfg.DefaultLanguage,
	}
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}

	// アトミック保存のため、一時ファイルを作成
	tmpFile, err := os.CreateTemp(dir, "config.*.json")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		_ = os.Remove(tmpPath)
	}()

	if err := tmpFile.Chmod(0600); err != nil {
		return err
	}

	if _, err := tmpFile.Write(data); err != nil {
		return err
	}

	if err := tmpFile.Sync(); err != nil {
		return err
	}

	if err := tmpFile.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}

	return nil
}
