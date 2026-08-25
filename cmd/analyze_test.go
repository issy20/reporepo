package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/issy20/reporepo/internal/clients"
	"github.com/issy20/reporepo/internal/core"
	"github.com/issy20/reporepo/internal/secretstore"
	"github.com/issy20/reporepo/internal/store"
	"github.com/issy20/reporepo/internal/testutil"
)

type recordingAIClient struct {
	analysis     *core.Analysis
	err          error
	calls        int
	lastLanguage string
}

func (c *recordingAIClient) Generate(_ context.Context, _ *core.RepoMeta, _, _, language string) (*core.Analysis, error) {
	c.calls++
	c.lastLanguage = language
	if c.err != nil {
		return nil, c.err
	}
	return c.analysis, nil
}

func testAnalysis() *core.Analysis {
	return &core.Analysis{
		Summary:       "AI summary",
		TechStack:     "Go, Cobra",
		Background:    "AI background",
		Keywords:      []string{"go", "cli"},
		Language:      "ja",
		Provider:      "claude",
		Model:         "model-x",
		PromptVersion: clients.PromptVersion,
		CreatedAt:     time.Now(),
	}
}

type analyzeDepsBuilder struct {
	t         *testing.T
	cfg       *core.Config
	clients   map[string]*recordingAIClient
	meta      *core.RepoMeta
	secrets   map[secretstore.Key]string
	storeErr  map[secretstore.Key]error
	githubErr error
}

func newAnalyzeDepsBuilder(t *testing.T, cfg *core.Config) *analyzeDepsBuilder {
	return &analyzeDepsBuilder{t: t, cfg: cfg, clients: map[string]*recordingAIClient{}}
}

func (b *analyzeDepsBuilder) provider(name string, ai *recordingAIClient) *analyzeDepsBuilder {
	b.clients[name] = ai
	return b
}

func (b *analyzeDepsBuilder) repositoryMeta(meta *core.RepoMeta) *analyzeDepsBuilder {
	b.meta = meta
	return b
}

func (b *analyzeDepsBuilder) secretStore(secrets map[secretstore.Key]string) *analyzeDepsBuilder {
	b.secrets = secrets
	return b
}

func (b *analyzeDepsBuilder) secretStoreErrors(errs map[secretstore.Key]error) *analyzeDepsBuilder {
	b.storeErr = errs
	return b
}

func (b *analyzeDepsBuilder) gitHubError(err error) *analyzeDepsBuilder {
	b.githubErr = err
	return b
}

func (b *analyzeDepsBuilder) build() (commandDependencies, string, *bytes.Buffer) {
	b.t.Helper()
	path := filepath.Join(b.t.TempDir(), "data.json")
	warn := &bytes.Buffer{}
	secrets := b.secrets
	if secrets == nil {
		secrets = map[secretstore.Key]string{
			secretstore.GitHubToken:     "github",
			secretstore.AnthropicAPIKey: "anthropic",
			secretstore.OpenAIAPIKey:    "openai",
			secretstore.GeminiAPIKey:    "gemini",
		}
	}
	store := testutil.NewMemorySecretStore(secrets)
	store.GetErrors = b.storeErr
	app := applicationDependencies{
		loadConfig:  func() (*core.Config, error) { return b.cfg, nil },
		secretStore: store,
		warn:        func(message string) { fmt.Fprintln(warn, message) },
		dataPath: func() (string, error) {
			return path, nil
		},
		newGitHub: func(*http.Client, string, string) clients.GitHubClient {
			meta := b.meta
			if meta == nil {
				meta = &core.RepoMeta{FullName: "owner/repo", Stars: 12345, Forks: 123, Language: "Go"}
			}
			return stubGitHubClient{meta: meta, err: b.githubErr}
		},
		newClaude: func(string, string, *http.Client) clients.AIClient { return providerClient(b.clients, "claude") },
		newOpenAI: func(string, string, *http.Client) clients.AIClient { return providerClient(b.clients, "openai") },
		newGemini: func(string, string) (clients.AIClient, error) {
			return providerClient(b.clients, "gemini"), nil
		},
	}
	return commandDependencies{app: &app, presenter: plainPresenter}, path, warn
}

func providerClient(clients map[string]*recordingAIClient, name string) clients.AIClient {
	if ai := clients[name]; ai != nil {
		return ai
	}
	return &recordingAIClient{}
}

func executeAnalyze(t *testing.T, deps commandDependencies, args ...string) (string, string, error) {
	t.Helper()
	root := newRootCommand(deps)
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetArgs(args)
	err := executeRoot(root, plainPresenter)
	return out.String(), errOut.String(), err
}

func TestAnalyzeWithoutArgumentReturnsError(t *testing.T) {
	b := newAnalyzeDepsBuilder(t, &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"}).provider("claude", &recordingAIClient{analysis: testAnalysis()})
	deps, _, _ := b.build()
	out, _, err := executeAnalyze(t, deps, "analyze")
	if err == nil {
		t.Fatal("analyze without argument error = nil")
	}
	if out != "" {
		t.Fatalf("stdout = %q, want empty", out)
	}
}

func TestAnalyzePublishesResultToStdout(t *testing.T) {
	ai := &recordingAIClient{analysis: testAnalysis()}
	b := newAnalyzeDepsBuilder(t, &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"}).provider("claude", ai)
	deps, _, _ := b.build()

	out, _, err := executeAnalyze(t, deps, "analyze", "owner/repo")
	if err != nil {
		t.Fatalf("analyze error = %v", err)
	}
	if !strings.Contains(out, "owner/repo") || !strings.Contains(out, "# Summary") || !strings.Contains(out, "AI summary") {
		t.Fatalf("stdout = %q", out)
	}
	if ai.calls != 1 {
		t.Fatalf("AI calls = %d, want 1", ai.calls)
	}
}

func TestAnalyzeHelpListsCommand(t *testing.T) {
	b := newAnalyzeDepsBuilder(t, &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"})
	deps, _, _ := b.build()
	root := newRootCommand(deps)
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "analyze") {
		t.Fatalf("help missing analyze: %q", out.String())
	}
}

func TestAnalyzeRejectsMultipleArguments(t *testing.T) {
	b := newAnalyzeDepsBuilder(t, &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"})
	deps, _, _ := b.build()
	_, _, err := executeAnalyze(t, deps, "analyze", "owner/repo", "extra")
	if err == nil {
		t.Fatal("analyze with two arguments error = nil")
	}
}

func TestAnalyzeProviderFlagOverridesDefault(t *testing.T) {
	claude := &recordingAIClient{analysis: testAnalysis()}
	openai := &recordingAIClient{analysis: testAnalysis()}
	b := newAnalyzeDepsBuilder(t, &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"})
	b.provider("claude", claude)
	b.provider("openai", openai)
	deps, _, _ := b.build()

	for _, args := range [][]string{
		{"analyze", "--provider", "openai", "owner/repo"},
		{"analyze", "-p", "openai", "owner/repo"},
	} {
		if _, _, err := executeAnalyze(t, deps, args...); err != nil {
			t.Fatalf("analyze %v error = %v", args, err)
		}
	}
	if openai.calls != 2 || claude.calls != 0 {
		t.Fatalf("provider calls: claude=%d openai=%d, want openai=2", claude.calls, openai.calls)
	}
}

func TestAnalyzeLanguageFlagOverridesDefault(t *testing.T) {
	ai := &recordingAIClient{analysis: testAnalysis()}
	b := newAnalyzeDepsBuilder(t, &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"}).provider("claude", ai)
	deps, _, _ := b.build()

	if _, _, err := executeAnalyze(t, deps, "analyze", "owner/repo"); err != nil {
		t.Fatal(err)
	}
	if ai.lastLanguage != "ja" {
		t.Fatalf("default language = %q, want ja", ai.lastLanguage)
	}

	if _, _, err := executeAnalyze(t, deps, "analyze", "--language", "en", "owner/repo"); err != nil {
		t.Fatal(err)
	}
	if ai.lastLanguage != "en" {
		t.Fatalf("flag language = %q, want en", ai.lastLanguage)
	}
}

func TestAnalyzeUnsetProviderReturnsConfigGuidance(t *testing.T) {
	secrets := map[secretstore.Key]string{
		secretstore.GitHubToken:     "github",
		secretstore.AnthropicAPIKey: "anthropic",
		secretstore.OpenAIAPIKey:    "openai",
	}
	b := newAnalyzeDepsBuilder(t, &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"})
	b.provider("claude", &recordingAIClient{analysis: testAnalysis()})
	b.provider("openai", &recordingAIClient{analysis: testAnalysis()})
	b.secretStore(secrets)
	deps, _, _ := b.build()

	_, _, err := executeAnalyze(t, deps, "analyze", "--provider", "gemini", "owner/repo")
	if err == nil || !strings.Contains(err.Error(), "API key が設定されていません") || !strings.Contains(err.Error(), "reporepo config") {
		t.Fatalf("error = %v", err)
	}
}

func TestAnalyzeRequiresAIKeysWhenNoneSet(t *testing.T) {
	b := newAnalyzeDepsBuilder(t, &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"})
	b.secretStore(map[secretstore.Key]string{
		secretstore.GitHubToken: "github",
	})
	deps, _, _ := b.build()

	_, _, err := executeAnalyze(t, deps, "analyze", "owner/repo")
	if err == nil || !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Fatalf("error = %v, want AI key requirement", err)
	}
}

func TestAnalyzeForceRegeneratesCache(t *testing.T) {
	ai := &recordingAIClient{analysis: testAnalysis()}
	b := newAnalyzeDepsBuilder(t, &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"}).provider("claude", ai)
	deps, _, _ := b.build()

	if _, _, err := executeAnalyze(t, deps, "analyze", "owner/repo"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := executeAnalyze(t, deps, "analyze", "--force", "owner/repo"); err != nil {
		t.Fatal(err)
	}
	if ai.calls != 2 {
		t.Fatalf("AI calls = %d, want 2 (force regenerates)", ai.calls)
	}
}

func TestAnalyzeJSONFlagOutputsJSON(t *testing.T) {
	b := newAnalyzeDepsBuilder(t, &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"})
	b.provider("claude", &recordingAIClient{analysis: testAnalysis()})
	deps, _, _ := b.build()

	out, _, err := executeAnalyze(t, deps, "analyze", "--json", "owner/repo")
	if err != nil {
		t.Fatalf("analyze --json error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%q", err, out)
	}
	for _, key := range []string{"full_name", "repo", "analysis", "language", "provider", "model", "created_at"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("JSON missing key %q: %v", key, decoded)
		}
	}
}

func TestAnalyzePlainOutputContainsMetaHeaderAndSections(t *testing.T) {
	analysis := testAnalysis()
	b := newAnalyzeDepsBuilder(t, &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"})
	b.provider("claude", &recordingAIClient{analysis: analysis})
	deps, _, _ := b.build()

	out, _, err := executeAnalyze(t, deps, "analyze", "owner/repo")
	if err != nil {
		t.Fatalf("analyze error = %v", err)
	}
	for _, want := range []string{"owner/repo", "⭐ 12345  Forks 123  Language Go", "取得: 今日  解析: 今日", "# Summary", "AI summary", "# Tech Stack", "Go, Cobra", "# Background", "AI background", "# Keywords", "go, cli"} {
		if !strings.Contains(out, want) {
			t.Errorf("plain output missing %q:\n%s", want, out)
		}
	}
}

func TestAnalyzePlainOutputSanitizesKeywordsANSI(t *testing.T) {
	analysis := testAnalysis()
	analysis.Keywords = []string{"go", "\x1b[31mANSI\x1b[0m"}
	b := newAnalyzeDepsBuilder(t, &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"})
	b.provider("claude", &recordingAIClient{analysis: analysis})
	deps, _, _ := b.build()

	out, _, err := executeAnalyze(t, deps, "analyze", "owner/repo")
	if err != nil {
		t.Fatalf("analyze error = %v", err)
	}
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("plain output contains ANSI escape in keywords: %q", out)
	}
	if !strings.Contains(out, "go") {
		t.Fatalf("plain output lost valid keyword: %q", out)
	}
}

func TestAnalyzePlainOutputSanitizesKeywordsControlChars(t *testing.T) {
	analysis := testAnalysis()
	analysis.Keywords = []string{"go", "cli\x00ctrl"}
	b := newAnalyzeDepsBuilder(t, &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"})
	b.provider("claude", &recordingAIClient{analysis: analysis})
	deps, _, _ := b.build()

	out, _, err := executeAnalyze(t, deps, "analyze", "owner/repo")
	if err != nil {
		t.Fatalf("analyze error = %v", err)
	}
	if strings.Contains(out, "\x00") {
		t.Fatalf("plain output contains control character: %q", out)
	}
	if !strings.Contains(out, "go") || !strings.Contains(out, "clictrl") {
		t.Fatalf("plain output lost keyword content: %q", out)
	}
}

func TestAnalyzePlainOutputContainsNoANSI(t *testing.T) {
	analysis := testAnalysis()
	analysis.Summary = "plain summary with \x1b[31mANSI\x1b[0m text"
	b := newAnalyzeDepsBuilder(t, &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"})
	b.provider("claude", &recordingAIClient{analysis: analysis})
	deps, _, _ := b.build()

	out, _, err := executeAnalyze(t, deps, "analyze", "owner/repo")
	if err != nil {
		t.Fatalf("analyze error = %v", err)
	}
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("plain output contains ANSI: %q", out)
	}
	if !strings.Contains(out, "ANSI") || !strings.Contains(out, "text") {
		t.Fatalf("plain output lost content: %q", out)
	}
}

func TestAnalyzeOutputExcludesSecrets(t *testing.T) {
	const secret = "super-secret-analyze-key"
	ai := &recordingAIClient{analysis: testAnalysis(), err: errors.New("upstream failure: " + secret)}
	b := newAnalyzeDepsBuilder(t, &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"})
	b.provider("claude", ai)
	b.secretStore(map[secretstore.Key]string{
		secretstore.GitHubToken:     "github",
		secretstore.AnthropicAPIKey: secret,
	})
	deps, _, _ := b.build()

	out, errOut, err := executeAnalyze(t, deps, "analyze", "owner/repo")
	if err == nil {
		t.Fatal("analyze error = nil, want upstream failure")
	}
	if strings.Contains(out+errOut+err.Error(), secret) {
		t.Fatalf("secret leaked in output: %q %q %v", out, errOut, err)
	}
}

func TestAnalyzeStaleReflectedInPlainAndJSON(t *testing.T) {
	analysis := testAnalysis()
	meta := &core.RepoMeta{
		FullName:  "owner/repo",
		Stars:     12345,
		Forks:     123,
		Language:  "Go",
		UpdatedAt: time.Now().Add(time.Hour),
	}
	b := newAnalyzeDepsBuilder(t, &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"})
	b.provider("claude", &recordingAIClient{analysis: analysis})
	b.repositoryMeta(meta)
	deps, _, _ := b.build()

	out, _, err := executeAnalyze(t, deps, "analyze", "owner/repo")
	if err != nil {
		t.Fatalf("analyze error = %v", err)
	}
	if !strings.Contains(out, "解析はリポジトリ更新前のものです（--force で再生成）") {
		t.Fatalf("plain output missing stale guidance:\n%s", out)
	}

	jsonOut, _, err := executeAnalyze(t, deps, "analyze", "--json", "owner/repo")
	if err != nil {
		t.Fatalf("analyze --json error = %v", err)
	}
	var decoded struct {
		Stale bool `json:"stale"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v\n%q", err, jsonOut)
	}
	if !decoded.Stale {
		t.Fatalf("JSON stale = false, want true: %q", jsonOut)
	}
}

func TestAnalyzeCacheHitDoesNotCallAI(t *testing.T) {
	ai := &recordingAIClient{analysis: testAnalysis()}
	b := newAnalyzeDepsBuilder(t, &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"}).provider("claude", ai)
	deps, _, _ := b.build()

	if _, _, err := executeAnalyze(t, deps, "analyze", "owner/repo"); err != nil {
		t.Fatal(err)
	}
	ai.calls = 0
	out, _, err := executeAnalyze(t, deps, "analyze", "owner/repo")
	if err != nil {
		t.Fatalf("cache-hit analyze error = %v", err)
	}
	if ai.calls != 0 {
		t.Fatalf("AI calls on cache hit = %d, want 0", ai.calls)
	}
	if !strings.Contains(out, "AI summary") {
		t.Fatalf("cached result not output: %q", out)
	}
}

func TestAnalyzeSavesResultToStore(t *testing.T) {
	ai := &recordingAIClient{analysis: testAnalysis()}
	b := newAnalyzeDepsBuilder(t, &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"}).provider("claude", ai)
	deps, path, _ := b.build()

	if _, _, err := executeAnalyze(t, deps, "analyze", "owner/repo"); err != nil {
		t.Fatal(err)
	}
	entries, err := store.NewStore(path).Load()
	if err != nil {
		t.Fatalf("store.Load() error = %v", err)
	}
	if len(entries) != 1 || entries[0].FullName != "owner/repo" {
		t.Fatalf("stored entries = %#v", entries)
	}
	if entries[0].Analyses["ja"] == nil || entries[0].Analyses["ja"].Summary != "AI summary" {
		t.Fatalf("stored analysis = %#v", entries[0].Analyses)
	}
}

func TestAnalyzeWarningsGoToErrOut(t *testing.T) {
	analysis := testAnalysis()
	b := newAnalyzeDepsBuilder(t, &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"})
	b.provider("claude", &recordingAIClient{analysis: analysis})
	b.gitHubError(errors.New("refresh failed"))
	deps, path, _ := b.build()

	entry := &core.Entry{
		FullName: "owner/repo",
		RepoMeta: &core.RepoMeta{FullName: "owner/repo", Stars: 1, Forks: 2, Language: "Go"},
		Analyses: map[string]*core.Analysis{"ja": analysis},
		ViewedAt: time.Now(),
	}
	if err := store.NewStore(path).Save([]*core.Entry{entry}); err != nil {
		t.Fatal(err)
	}

	out, errOut, err := executeAnalyze(t, deps, "analyze", "owner/repo")
	if err != nil {
		t.Fatalf("cache-hit analyze error = %v", err)
	}
	if !strings.Contains(errOut, "警告: GitHub からメタ情報を取得できませんでした") {
		t.Fatalf("stderr = %q, want warning with prefix", errOut)
	}
	if strings.Contains(out, "GitHub からメタ情報") {
		t.Fatalf("warning leaked into stdout: %q", out)
	}
	if !strings.Contains(out, "AI summary") {
		t.Fatalf("cached result missing from stdout: %q", out)
	}
}

func TestAnalyzeSecretWarningsGoToErrOut(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	b := newAnalyzeDepsBuilder(t, &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"})
	b.provider("claude", &recordingAIClient{analysis: testAnalysis()})
	b.secretStoreErrors(map[secretstore.Key]error{
		secretstore.GitHubToken: errors.New("backend down"),
	})
	deps, _, _ := b.build()

	out, errOut, err := executeAnalyze(t, deps, "analyze", "owner/repo")
	if err != nil {
		t.Fatalf("analyze error = %v", err)
	}
	if !strings.Contains(errOut, "警告: GitHub tokenをOS資格情報ストアから読み込めませんでした") {
		t.Fatalf("stderr = %q, want secret resolution warning", errOut)
	}
	if strings.Contains(out, "GitHub token") {
		t.Fatalf("secret warning leaked into stdout: %q", out)
	}
	if !strings.Contains(out, "AI summary") {
		t.Fatalf("analysis missing from stdout: %q", out)
	}
}

func TestAnalyzeInvalidRepositoryFormatReturnsSafeError(t *testing.T) {
	b := newAnalyzeDepsBuilder(t, &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"})
	b.provider("claude", &recordingAIClient{analysis: testAnalysis()})
	deps, _, _ := b.build()

	out, errOut, err := executeAnalyze(t, deps, "analyze", "not-a-repo")
	if err == nil || !strings.Contains(err.Error(), "リポジトリの入力形式が正しくありません") {
		t.Fatalf("error = %v", err)
	}
	if out != "" {
		t.Fatalf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, "ERROR:") {
		t.Fatalf("stderr = %q, want rendered error", errOut)
	}
}

func TestAnalyzeGenerationFailureReturnsSafeError(t *testing.T) {
	const sensitive = "upstream-boom"
	ai := &recordingAIClient{analysis: testAnalysis(), err: errors.New(sensitive)}
	b := newAnalyzeDepsBuilder(t, &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"})
	b.provider("claude", ai)
	deps, _, _ := b.build()

	out, errOut, err := executeAnalyze(t, deps, "analyze", "owner/repo")
	if err == nil || !strings.Contains(err.Error(), "AI による解析に失敗しました") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(out+errOut+err.Error(), sensitive) {
		t.Fatalf("sensitive detail leaked: %q %q %v", out, errOut, err)
	}
	if out != "" {
		t.Fatalf("stdout = %q, want empty on failure", out)
	}
}
