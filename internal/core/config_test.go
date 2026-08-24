package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigMarshalDoesNotContainSecrets(t *testing.T) {
	cfg := Config{
		GithubToken:     "github-sensitive-value",
		AnthropicAPIKey: "anthropic-sensitive-value",
		OpenAIAPIKey:    "openai-sensitive-value",
		GeminiAPIKey:    "gemini-sensitive-value",
		DefaultProvider: "claude",
		DefaultLanguage: "ja",
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	for _, forbidden := range []string{
		"github_token", "anthropic_api_key", "openai_api_key", "gemini_api_key",
		cfg.GithubToken, cfg.AnthropicAPIKey, cfg.OpenAIAPIKey, cfg.GeminiAPIKey,
	} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("marshaled Config contains secret material")
		}
	}
}

func TestLoadConfigFileSeparatesLegacySecrets(t *testing.T) {
	tempDir := t.TempDir()
	origPath := configFilePath
	configFilePath = filepath.Join(tempDir, "config.json")
	defer func() { configFilePath = origPath }()

	legacyJSON := `{
  "github_token": "legacy-github",
  "anthropic_api_key": "legacy-anthropic",
  "openai_api_key": "legacy-openai",
  "gemini_api_key": "legacy-gemini",
  "default_provider": "openai",
  "default_language": "en"
}`
	if err := os.WriteFile(configFilePath, []byte(legacyJSON), 0600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	cfg, legacy, err := LoadConfigFile()
	if err != nil {
		t.Fatalf("LoadConfigFile() error = %v", err)
	}
	if cfg.GithubToken != "" || cfg.AnthropicAPIKey != "" || cfg.OpenAIAPIKey != "" || cfg.GeminiAPIKey != "" {
		t.Fatal("LoadConfigFile() copied legacy secrets into runtime Config")
	}
	if cfg.DefaultProvider != "openai" || cfg.DefaultLanguage != "en" {
		t.Fatalf("LoadConfigFile() config = %#v", cfg)
	}
	wantLegacy := LegacySecrets{
		GithubToken:     "legacy-github",
		AnthropicAPIKey: "legacy-anthropic",
		OpenAIAPIKey:    "legacy-openai",
		GeminiAPIKey:    "legacy-gemini",
	}
	if legacy != wantLegacy {
		t.Fatal("LoadConfigFile() did not separate all legacy secrets")
	}
}

func TestLoadConfigFileMissingReturnsDefaultsAndNoLegacySecrets(t *testing.T) {
	tempDir := t.TempDir()
	origPath := configFilePath
	configFilePath = filepath.Join(tempDir, "missing.json")
	defer func() { configFilePath = origPath }()

	cfg, legacy, err := LoadConfigFile()
	if err != nil {
		t.Fatalf("LoadConfigFile() error = %v", err)
	}
	if cfg.DefaultProvider != "claude" || cfg.DefaultLanguage != "ja" {
		t.Fatalf("LoadConfigFile() config = %#v, want defaults", cfg)
	}
	if legacy != (LegacySecrets{}) {
		t.Fatal("LoadConfigFile() returned legacy secrets for a missing file")
	}
}

func TestLoadConfigFileRejectsBrokenJSON(t *testing.T) {
	tempDir := t.TempDir()
	origPath := configFilePath
	configFilePath = filepath.Join(tempDir, "config.json")
	defer func() { configFilePath = origPath }()
	if err := os.WriteFile(configFilePath, []byte(`{"default_provider":`), 0600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	if _, _, err := LoadConfigFile(); err == nil {
		t.Fatal("LoadConfigFile() error = nil, want malformed JSON error")
	}
}

func TestLoadConfigFileKeepsDefaultsForMissingFields(t *testing.T) {
	tempDir := t.TempDir()
	origPath := configFilePath
	configFilePath = filepath.Join(tempDir, "config.json")
	defer func() { configFilePath = origPath }()
	if err := os.WriteFile(configFilePath, []byte(`{}`), 0600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	cfg, _, err := LoadConfigFile()
	if err != nil {
		t.Fatalf("LoadConfigFile() error = %v", err)
	}
	if cfg.DefaultProvider != "claude" || cfg.DefaultLanguage != "ja" {
		t.Fatalf("LoadConfigFile() config = %#v, want defaults", cfg)
	}
}

func TestLoadStoredConfig_Default(t *testing.T) {
	tempDir := t.TempDir()
	origPath := configFilePath
	configFilePath = filepath.Join(tempDir, "config.json")
	defer func() { configFilePath = origPath }()

	cfg, err := LoadStoredConfig()
	if err != nil {
		t.Fatalf("LoadStoredConfig failed: %v", err)
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

	loaded, err := LoadStoredConfig()
	if err != nil {
		t.Fatalf("LoadStoredConfig after save failed: %v", err)
	}
	if loaded.GithubToken != "" || loaded.AnthropicAPIKey != "" || loaded.OpenAIAPIKey != "" || loaded.DefaultProvider != "openai" {
		t.Errorf("loaded config does not match saved config")
	}
	data, err := os.ReadFile(configFilePath)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	for _, forbidden := range []string{"github_token", "anthropic_api_key", "openai_api_key", "test-token", "claude-key", "openai-key"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatal("saved config contains secret material")
		}
	}
}

func TestLoadStoredConfig_DoesNotApplyEnvironment(t *testing.T) {
	tempDir := t.TempDir()
	origPath := configFilePath
	configFilePath = filepath.Join(tempDir, "config.json")
	defer func() { configFilePath = origPath }()

	want := &Config{
		DefaultProvider: "claude",
		DefaultLanguage: "ja",
	}
	if err := SaveConfig(want); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	t.Setenv("GITHUB_TOKEN", "env-token")
	t.Setenv("ANTHROPIC_API_KEY", "env-claude")
	t.Setenv("OPENAI_API_KEY", "env-openai")

	got, err := LoadStoredConfig()
	if err != nil {
		t.Fatalf("LoadStoredConfig() error = %v", err)
	}
	if *got != *want {
		t.Fatalf("LoadStoredConfig() = %#v, want %#v", got, want)
	}
}

func TestLoadConfig_NoCache(t *testing.T) {
	origPath := configFilePath
	configFilePath = ""
	defer func() { configFilePath = origPath }()

	origHome := os.Getenv("HOME")
	origConfigHome := os.Getenv("XDG_CONFIG_HOME")
	origAppData := os.Getenv("APPDATA")
	defer func() {
		os.Setenv("HOME", origHome)
		os.Setenv("XDG_CONFIG_HOME", origConfigHome)
		os.Setenv("APPDATA", origAppData)
	}()

	// os.UserConfigDir は環境で優先される変数が異なる（Unix: XDG_CONFIG_HOME→HOME / darwin: HOME / Windows: APPDATA）。
	// 各プラットフォームの優先変数だけを変えて、結果が変わることを確認する。
	setHome := func(dir string) {
		os.Setenv("HOME", dir)
		os.Setenv("XDG_CONFIG_HOME", dir+"/config")
		os.Setenv("APPDATA", dir+"/appdata")
	}

	setHome("/tmp/home1")
	path1, err := resolveConfigPath()
	if err != nil {
		t.Fatalf("resolveConfigPath failed: %v", err)
	}

	// 2回目でユーザー設定ディレクトリの基準を変えた時、結果が変わることを確認する（キャッシュされていないことの検証）
	setHome("/tmp/home2")

	// テスト用にキャッシュ変数を明示的にクリアしない状態にする
	// もし resolveConfigPath() が内部でキャッシュを持っていれば同じ値が返るはず。
	path2, err := resolveConfigPath()
	if err != nil {
		t.Fatalf("resolveConfigPath failed: %v", err)
	}

	if path1 == path2 {
		t.Errorf("expected paths to be different after changing user config dir, but got the same: %s", path1)
	}
}
