package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/issy20/reporepo/internal/clients"
	"github.com/issy20/reporepo/internal/core"
	"github.com/issy20/reporepo/internal/testutil"
)

type trendingRecordingGitHub struct {
	calls int
	query clients.TrendingQuery
	repos []clients.TrendingRepo
	err   error
}

func (g *trendingRecordingGitHub) FetchRepository(context.Context, string, string) (*clients.RepositoryData, error) {
	return nil, nil
}

func (g *trendingRecordingGitHub) FetchRepositoryMeta(context.Context, string, string) (*core.RepoMeta, error) {
	return nil, nil
}

func (g *trendingRecordingGitHub) SearchTrending(_ context.Context, q clients.TrendingQuery) ([]clients.TrendingRepo, error) {
	g.calls++
	g.query = q
	if g.err != nil {
		return nil, g.err
	}
	return g.repos, nil
}

func trendingDeps(t *testing.T, gh clients.GitHubClient) (commandDependencies, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data.json")
	app := applicationDependencies{
		loadConfig: func() (*core.Config, error) {
			return &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"}, nil
		},
		secretStore: testutil.NewMemorySecretStore(nil),
		warn:        func(string) {},
		dataPath:    func() (string, error) { return path, nil },
		newGitHub:   func(*http.Client, string, string) clients.GitHubClient { return gh },
	}
	return commandDependencies{app: &app, presenter: plainPresenter}, path
}

func executeTrending(t *testing.T, deps commandDependencies, args ...string) (string, string, error) {
	t.Helper()
	root := newRootCommand(deps)
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetArgs(args)
	err := executeRoot(root, plainPresenter)
	return out.String(), errOut.String(), err
}

func runTrendingNow(t *testing.T, deps commandDependencies, since, language string, minStars int, jsonOutput bool, now time.Time) (string, string, error) {
	t.Helper()
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	err := runTrending(deps, since, language, minStars, jsonOutput, func() time.Time { return now }, out, errOut)
	return out.String(), errOut.String(), err
}

func TestTrendingOutputsOneRepoPerLine(t *testing.T) {
	gh := &trendingRecordingGitHub{repos: []clients.TrendingRepo{
		{FullName: "owner/repo", Description: "A repo", Stars: 123, Language: "Go"},
		{FullName: "other/repo", Description: "Another", Stars: 456, Language: "Rust"},
	}}
	deps, _ := trendingDeps(t, gh)
	out, errOut, err := executeTrending(t, deps, "trending")
	if err != nil {
		t.Fatalf("trending error = %v", err)
	}
	if !strings.Contains(out, "owner/repo ⭐ 123  A repo  Go") {
		t.Fatalf("stdout = %q", out)
	}
	if !strings.Contains(out, "other/repo ⭐ 456  Another  Rust") {
		t.Fatalf("stdout = %q", out)
	}
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("line count = %d, want 2:\n%s", strings.Count(out, "\n"), out)
	}
	if errOut != "" {
		t.Fatalf("stderr = %q, want empty", errOut)
	}
}

func TestTrendingOutputSanitizesControlCharsAndANSI(t *testing.T) {
	gh := &trendingRecordingGitHub{repos: []clients.TrendingRepo{
		{FullName: "owner/repo", Description: "bad\x1b[31mANSI\x1b[0m\x00ctrl", Stars: 1, Language: "Go"},
	}}
	deps, _ := trendingDeps(t, gh)
	out, _, err := executeTrending(t, deps, "trending")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "\x1b[") || strings.Contains(out, "\x00") {
		t.Fatalf("output contains control characters: %q", out)
	}
	if !strings.Contains(out, "ANSI") || !strings.Contains(out, "ctrl") {
		t.Fatalf("output lost content: %q", out)
	}
}

func TestTrendingJSONOutput(t *testing.T) {
	gh := &trendingRecordingGitHub{repos: []clients.TrendingRepo{
		{FullName: "owner/repo", Description: "desc", Stars: 123, Language: "Go"},
	}}
	deps, _ := trendingDeps(t, gh)
	out, _, err := executeTrending(t, deps, "trending", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%q", err, out)
	}
	if len(decoded) != 1 || decoded[0]["full_name"] != "owner/repo" || decoded[0]["stars"] != float64(123) {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func TestTrendingFlagsReflectedInQuery(t *testing.T) {
	gh := &trendingRecordingGitHub{}
	deps, _ := trendingDeps(t, gh)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	_, _, err := runTrendingNow(t, deps, "today", "go", 100, false, now)
	if err != nil {
		t.Fatal(err)
	}
	wantAfter := now.AddDate(0, 0, -1)
	if !gh.query.CreatedAfter.Equal(wantAfter) {
		t.Fatalf("CreatedAfter = %v, want %v", gh.query.CreatedAfter, wantAfter)
	}
	if gh.query.Language != "go" || gh.query.MinStars != 100 {
		t.Fatalf("query = %#v", gh.query)
	}
}

func TestTrendingInvalidSinceReturnsError(t *testing.T) {
	deps, _ := trendingDeps(t, &trendingRecordingGitHub{})
	out, errOut, err := executeTrending(t, deps, "trending", "--since", "decade")
	if err == nil || !strings.Contains(err.Error(), "since は today") {
		t.Fatalf("error = %v", err)
	}
	if out != "" || !strings.Contains(errOut, "ERROR:") {
		t.Fatalf("out = %q, errOut = %q", out, errOut)
	}
}

func TestTrendingWorksWithoutAIKeys(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	gh := &trendingRecordingGitHub{repos: testRepos()}
	deps, _ := trendingDeps(t, gh)
	out, _, err := executeTrending(t, deps, "trending")
	if err != nil {
		t.Fatalf("trending error = %v", err)
	}
	if !strings.Contains(out, "owner/repo") {
		t.Fatalf("stdout = %q", out)
	}
}

func TestTrendingFreshCacheDoesNotRefetch(t *testing.T) {
	now := time.Now()
	gh := &trendingRecordingGitHub{repos: testRepos()}
	deps, _ := trendingDeps(t, gh)
	if _, _, err := runTrendingNow(t, deps, "week", "", 50, false, now); err != nil {
		t.Fatal(err)
	}
	if gh.calls != 1 {
		t.Fatalf("SearchTrending calls = %d, want 1", gh.calls)
	}
	out, _, err := runTrendingNow(t, deps, "week", "", 50, false, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if gh.calls != 1 {
		t.Fatalf("SearchTrending calls on cache hit = %d, want 1", gh.calls)
	}
	if !strings.Contains(out, "owner/repo") {
		t.Fatalf("stdout = %q", out)
	}
}

func TestTrendingExpiredCacheRefetches(t *testing.T) {
	now := time.Now()
	gh := &trendingRecordingGitHub{repos: testRepos()}
	deps, _ := trendingDeps(t, gh)
	if _, _, err := runTrendingNow(t, deps, "week", "", 50, false, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runTrendingNow(t, deps, "week", "", 50, false, now.Add(DefaultTrendingCacheTTL+time.Hour)); err != nil {
		t.Fatal(err)
	}
	if gh.calls != 2 {
		t.Fatalf("SearchTrending calls = %d, want 2", gh.calls)
	}
}

func TestTrendingRateLimitFallbackShowsStaleCache(t *testing.T) {
	now := time.Now()
	gh := &trendingRecordingGitHub{repos: testRepos()}
	deps, _ := trendingDeps(t, gh)
	if _, _, err := runTrendingNow(t, deps, "week", "", 50, false, now); err != nil {
		t.Fatal(err)
	}
	gh.err = clients.ErrTrendingRateLimited
	out, errOut, err := runTrendingNow(t, deps, "week", "", 50, false, now.Add(DefaultTrendingCacheTTL+time.Hour))
	if err != nil {
		t.Fatalf("trending error = %v, want fallback success", err)
	}
	if !strings.Contains(out, "owner/repo") {
		t.Fatalf("stdout = %q, want cached repos", out)
	}
	if !strings.Contains(errOut, "警告:") || !strings.Contains(errOut, "レート制限") {
		t.Fatalf("stderr = %q, want rate limit warning", errOut)
	}
}

func TestTrendingRateLimitWithoutCacheReturnsSafeError(t *testing.T) {
	gh := &trendingRecordingGitHub{err: clients.ErrTrendingRateLimited}
	deps, _ := trendingDeps(t, gh)
	out, errOut, err := executeTrending(t, deps, "trending")
	if err == nil || !strings.Contains(err.Error(), "レート制限") {
		t.Fatalf("error = %v", err)
	}
	if out != "" {
		t.Fatalf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, "ERROR:") {
		t.Fatalf("stderr = %q, want rendered error", errOut)
	}
}

func TestTrendingOutputExcludesSecrets(t *testing.T) {
	const secret = "super-secret-trending"
	gh := &trendingRecordingGitHub{err: errors.New("upstream failure: " + secret)}
	deps, _ := trendingDeps(t, gh)
	out, errOut, err := executeTrending(t, deps, "trending")
	if err == nil {
		t.Fatal("trending error = nil")
	}
	if strings.Contains(out+errOut+err.Error(), secret) {
		t.Fatalf("secret leaked: %q %q %v", out, errOut, err)
	}
}

func TestTrendingCommandIsRegistered(t *testing.T) {
	root := NewRootCommand()
	found := false
	for _, command := range root.Commands() {
		if command.Name() == "trending" {
			found = true
		}
	}
	if !found {
		t.Fatal("trending command not registered")
	}
}

func TestTrendingHelpListsCommand(t *testing.T) {
	root := newRootCommand(commandDependencies{presenter: plainPresenter})
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "trending") {
		t.Fatalf("help missing trending: %q", out.String())
	}
}

func TestTrendingRejectsArguments(t *testing.T) {
	deps, _ := trendingDeps(t, &trendingRecordingGitHub{})
	_, _, err := executeTrending(t, deps, "trending", "owner/repo")
	if err == nil {
		t.Fatal("trending with argument error = nil")
	}
}

func TestTrendingFetchFailureReturnsSafeError(t *testing.T) {
	const sensitive = "upstream-boom"
	gh := &trendingRecordingGitHub{err: errors.New(sensitive)}
	deps, _ := trendingDeps(t, gh)
	out, errOut, err := executeTrending(t, deps, "trending")
	if err == nil || !strings.Contains(err.Error(), "取得できませんでした") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(out+errOut+err.Error(), sensitive) {
		t.Fatalf("sensitive detail leaked: %q %q %v", out, errOut, err)
	}
	if out != "" {
		t.Fatalf("stdout = %q, want empty on failure", out)
	}
}

func TestTrendingCacheSaveFailureIsWarningNotError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(blocker, "data.json")
	app := applicationDependencies{
		loadConfig: func() (*core.Config, error) {
			return &core.Config{DefaultProvider: "claude", DefaultLanguage: "ja"}, nil
		},
		secretStore: testutil.NewMemorySecretStore(nil),
		warn:        func(string) {},
		dataPath:    func() (string, error) { return path, nil },
		newGitHub: func(*http.Client, string, string) clients.GitHubClient {
			return &trendingRecordingGitHub{repos: testRepos()}
		},
	}
	deps := commandDependencies{app: &app, presenter: plainPresenter}
	out, errOut, err := runTrendingNow(t, deps, "week", "", 50, false, time.Now())
	if err != nil {
		t.Fatalf("trending error = %v", err)
	}
	if !strings.Contains(out, "owner/repo") {
		t.Fatalf("stdout = %q", out)
	}
	if !strings.Contains(errOut, "警告: トレンド一覧のキャッシュを保存できませんでした") {
		t.Fatalf("stderr = %q, want cache save warning", errOut)
	}
}
