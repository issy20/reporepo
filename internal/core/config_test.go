package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_Default(t *testing.T) {
	tempDir := t.TempDir()
	origPath := configFilePath
	configFilePath = filepath.Join(tempDir, "config.json")
	defer func() { configFilePath = origPath }()

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.DefaultProvider != "claude" {
		t.Errorf("expected default provider 'claude', got '%s'", cfg.DefaultProvider)
	}
	if cfg.DefaultLanguage != "ja" {
		t.Errorf("expected default language 'ja', got '%s'", cfg.DefaultLanguage)
	}
}

func TestSaveConfig_Permissions(t *testing.T) {
	tempDir := t.TempDir()
	origPath := configFilePath
	configFilePath = filepath.Join(tempDir, "config.json")
	defer func() { configFilePath = origPath }()

	cfg := &Config{
		GithubToken:     "test-token",
		AnthropicAPIKey: "claude-key",
		OpenAIAPIKey:    "openai-key",
		DefaultProvider: "openai",
		DefaultLanguage: "en",
	}

	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	info, err := os.Stat(configFilePath)
	if err != nil {
		t.Fatalf("config file stat failed: %v", err)
	}

	mode := info.Mode().Perm()
	if mode != 0600 {
		t.Errorf("expected file mode 0600, got %o", mode)
	}

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig after save failed: %v", err)
	}
	if loaded.GithubToken != "test-token" || loaded.DefaultProvider != "openai" {
		t.Errorf("loaded config does not match saved config")
	}
}

func TestLoadConfig_EnvPriority(t *testing.T) {
	tempDir := t.TempDir()
	origPath := configFilePath
	configFilePath = filepath.Join(tempDir, "config.json")
	defer func() { configFilePath = origPath }()

	cfg := &Config{
		GithubToken:     "file-token",
		AnthropicAPIKey: "file-claude",
		OpenAIAPIKey:    "file-openai",
	}
	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	t.Setenv("GITHUB_TOKEN", "env-token")
	t.Setenv("ANTHROPIC_API_KEY", "env-claude")
	t.Setenv("OPENAI_API_KEY", "env-openai")

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if loaded.GithubToken != "env-token" {
		t.Errorf("expected GITHUB_TOKEN to be 'env-token' (env), got '%s'", loaded.GithubToken)
	}
	if loaded.AnthropicAPIKey != "env-claude" {
		t.Errorf("expected ANTHROPIC_API_KEY to be 'env-claude' (env), got '%s'", loaded.AnthropicAPIKey)
	}
	if loaded.OpenAIAPIKey != "env-openai" {
		t.Errorf("expected OPENAI_API_KEY to be 'env-openai' (env), got '%s'", loaded.OpenAIAPIKey)
	}
}

func TestLoadConfig_NoCache(t *testing.T) {
	origPath := configFilePath
	configFilePath = ""
	defer func() { configFilePath = origPath }()

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	os.Setenv("HOME", "/tmp/home1")
	path1, err := resolveConfigPath()
	if err != nil {
		t.Fatalf("resolveConfigPath failed: %v", err)
	}

	// 2回目で HOME を変えた時、結果が変わることを確認する（キャッシュされていないことの検証）
	os.Setenv("HOME", "/tmp/home2")

	// テスト用にキャッシュ変数を明示的にクリアしない状態にする
	// もし resolveConfigPath() が内部でキャッシュを持っていれば同じ値が返るはず。
	path2, err := resolveConfigPath()
	if err != nil {
		t.Fatalf("resolveConfigPath failed: %v", err)
	}

	if path1 == path2 {
		t.Errorf("expected paths to be different after changing HOME, but got the same: %s", path1)
	}
}
