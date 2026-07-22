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
	GithubToken     string `json:"github_token"`
	AnthropicAPIKey string `json:"anthropic_api_key"`
	OpenAIAPIKey    string `json:"openai_api_key"`
	DefaultProvider string `json:"default_provider"`
	DefaultLanguage string `json:"default_language"`
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

// LoadConfig は設定ファイルを読み込み、環境変数で上書きした Config を返す。
func LoadConfig() (*Config, error) {
	path, err := resolveConfigPath()
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		DefaultProvider: "claude",
		DefaultLanguage: "ja",
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			overrideFromEnv(cfg)
			return cfg, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	overrideFromEnv(cfg)
	return cfg, nil
}

// overrideFromEnv は環境変数が設定されていれば Config を上書きする。
func overrideFromEnv(cfg *Config) {
	if env := os.Getenv("GITHUB_TOKEN"); env != "" {
		cfg.GithubToken = env
	}
	if env := os.Getenv("ANTHROPIC_API_KEY"); env != "" {
		cfg.AnthropicAPIKey = env
	}
	if env := os.Getenv("OPENAI_API_KEY"); env != "" {
		cfg.OpenAIAPIKey = env
	}
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

	data, err := json.MarshalIndent(cfg, "", "  ")
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
