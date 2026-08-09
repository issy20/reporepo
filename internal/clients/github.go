package clients

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/issy20/reporepo/internal/core"
)

var (
	ErrNotFound    = errors.New("repository not found")
	ErrRateLimited = errors.New("github rate limit exceeded")
)

const maxREADMEBytes = 4 << 20 // 4 MiB

// コード文脈の選定上限。
const (
	maxCodeFiles         = 6
	maxCodeCharacters    = 8000
	maxCodeFileBytes     = 256 << 10 // 256 KiB
	maxTreeResponseBytes = 8 << 20   // 8 MiB
	maxCodeFileReadBytes = 1 << 20   // 1 MiB
)

var (
	// github.com/owner/repo
	urlRegex = regexp.MustCompile(`^(?:https?://)?github\.com/([^/]+)/([^/]+?)(?:\.git)?/?$`)
	// git@github.com:owner/repo.git
	sshRegex     = regexp.MustCompile(`^git@github\.com:([^/]+)/([^/]+?)(?:\.git)?$`)
	segmentRegex = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

type RepositoryData struct {
	Meta   *core.RepoMeta
	README string
	Code   *CodeContext // nil ならコード文脈なし（フォールバック）
}

// CodeFile は選定されたソースファイル1件。
type CodeFile struct {
	Path    string
	Content string
}

// CodeContext は AI 入力に使うコード文脈。Files が空でも nil でなければコード文脈あり。
type CodeContext struct {
	Files []CodeFile
}

// treeEntry は GitHub ツリー応答の blob/tree エントリ。
type treeEntry struct {
	Path string
	Size int64
	Type string
}

type GitHubClient interface {
	FetchRepository(ctx context.Context, owner, repo string) (*RepositoryData, error)
	FetchRepositoryMeta(ctx context.Context, owner, repo string) (*core.RepoMeta, error)
}

type Client struct {
	httpClient *http.Client
	baseURL    string
	token      string
}

func NewGitHubClient(httpClient *http.Client, baseURL, token string) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{httpClient: httpClient, baseURL: baseURL, token: token}
}

func ParseRepositoryInput(input string) (owner, repo string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", errors.New("empty input")
	}

	// 不正な文字のチェック (空白, tab, newline, ?, #, %2f などの無効な文字)
	if strings.ContainsAny(input, " \t\r\n?#") || strings.Contains(strings.ToLower(input), "%2f") {
		return "", "", errors.New("invalid characters in input")
	}

	// 1. SSH 形式のチェック
	if strings.HasPrefix(input, "git@") {
		matches := sshRegex.FindStringSubmatch(input)
		if len(matches) == 3 && isValidSegment(matches[1]) && isValidSegment(matches[2]) {
			return matches[1], matches[2], nil
		}
		return "", "", errors.New("invalid SSH format")
	}

	// 2. HTTP/HTTPS 形式のチェック (github.com を含む場合)
	if strings.Contains(input, "github.com/") {
		matches := urlRegex.FindStringSubmatch(input)
		if len(matches) == 3 && isValidSegment(matches[1]) && isValidSegment(matches[2]) {
			return matches[1], matches[2], nil
		}
		return "", "", errors.New("invalid GitHub URL format")
	}

	// 3. 他のホストのチェック
	if strings.Contains(input, "://") {
		return "", "", errors.New("unsupported host")
	}

	// 4. ショート形式 (owner/repo) のチェック
	parts := strings.Split(input, "/")
	if len(parts) == 2 && isValidSegment(parts[0]) && isValidSegment(parts[1]) {
		return parts[0], parts[1], nil
	}

	return "", "", errors.New("invalid repository format")
}

type githubRepoMeta struct {
	FullName        string   `json:"full_name"`
	Description     string   `json:"description"`
	StargazersCount int      `json:"stargazers_count"`
	ForksCount      int      `json:"forks_count"`
	Language        string   `json:"language"`
	Topics          []string `json:"topics"`
	HTMLURL         string   `json:"html_url"`
	License         *struct {
		SpdxID string `json:"spdx_id"`
	} `json:"license"`
	DefaultBranch string    `json:"default_branch"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (c *Client) sendRequest(ctx context.Context, method, path string, headers map[string]string) (*http.Response, error) {
	url := fmt.Sprintf("%s%s", strings.TrimSuffix(c.baseURL, "/"), path)
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	if c.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func handleResponseError(resp *http.Response) error {
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode == http.StatusForbidden {
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return ErrRateLimited
		}
	}
	return &HTTPError{StatusCode: resp.StatusCode}
}

func isValidSegment(s string) bool {
	if s == "" {
		return false
	}
	if s == "." || s == ".." {
		return false
	}
	return segmentRegex.MatchString(s)
}

// manifestOrder は優先1のマニフェスト定義順。
var manifestOrder = []string{
	"go.mod",
	"package.json",
	"Cargo.toml",
	"pyproject.toml",
	"requirements.txt",
	"setup.py",
	"composer.json",
	"Gemfile",
	"pom.xml",
	"build.gradle",
	"mix.exs",
}

// excludedDirectories は選定対象から除外する依存・生成物ディレクトリ。
var excludedDirectories = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	".git":         true,
	".venv":        true,
	".idea":        true,
	"target":       true,
	"out":          true,
}

// excludedFiles は選定対象から除外するロック・生成ファイル（ベース名）。
var excludedFiles = map[string]bool{
	"package-lock.json": true,
	"yarn.lock":         true,
	"pnpm-lock.yaml":    true,
	"go.sum":            true,
	"Cargo.lock":        true,
	"Gemfile.lock":      true,
	"composer.lock":     true,
}

func pathBase(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}

func manifestRank(path string) (int, bool) {
	base := pathBase(path)
	for i, name := range manifestOrder {
		if base == name {
			return i, true
		}
	}
	return 0, false
}

func isEntryPoint(path string) bool {
	if pathBase(path) == "main.go" {
		return true
	}
	if strings.HasPrefix(path, "cmd/") {
		return true
	}
	if strings.HasPrefix(path, "src/main.") || strings.HasPrefix(path, "lib/main.") {
		return true
	}
	base := pathBase(path)
	return strings.HasPrefix(base, "index.") || strings.HasPrefix(base, "cli.")
}

func excludedPath(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if excludedDirectories[segment] {
			return true
		}
	}
	return false
}

func excludedFile(path string) bool {
	base := pathBase(path)
	if excludedFiles[base] {
		return true
	}
	return strings.HasSuffix(base, ".min.js") ||
		strings.HasSuffix(base, ".min.css") ||
		strings.HasSuffix(base, ".map")
}

func codeFileRank(path string) (class int, order int) {
	if idx, ok := manifestRank(path); ok {
		return 1, idx
	}
	if isEntryPoint(path) {
		return 2, 0
	}
	return 3, 0
}

// selectCodeFiles はツリーエントリから決定的に取得対象パスを選定する。
func selectCodeFiles(entries []treeEntry) []string {
	candidates := make([]treeEntry, 0, len(entries))
	for _, e := range entries {
		if e.Type != "" && e.Type != "blob" {
			continue
		}
		if e.Size > maxCodeFileBytes {
			continue
		}
		if excludedPath(e.Path) || excludedFile(e.Path) {
			continue
		}
		candidates = append(candidates, e)
	}

	sort.Slice(candidates, func(i, j int) bool {
		ci, oi := codeFileRank(candidates[i].Path)
		cj, oj := codeFileRank(candidates[j].Path)
		if ci != cj {
			return ci < cj
		}
		if ci == 1 {
			if oi != oj {
				return oi < oj
			}
			return candidates[i].Path < candidates[j].Path
		}
		if ci == 2 {
			return candidates[i].Path < candidates[j].Path
		}
		if candidates[i].Size != candidates[j].Size {
			return candidates[i].Size < candidates[j].Size
		}
		return candidates[i].Path < candidates[j].Path
	})

	selected := make([]string, 0, len(candidates))
	totalChars := 0
	for _, e := range candidates {
		if len(selected) >= maxCodeFiles || totalChars >= maxCodeCharacters {
			break
		}
		selected = append(selected, e.Path)
		totalChars += int(e.Size)
	}
	return selected
}

// repoMetaFromGitHub は GitHub の /repos 応答を RepoMeta へ変換する。Languages は空のまま。
func repoMetaFromGitHub(g githubRepoMeta) *core.RepoMeta {
	meta := &core.RepoMeta{
		FullName:    g.FullName,
		Description: g.Description,
		Stars:       g.StargazersCount,
		Forks:       g.ForksCount,
		Language:    g.Language,
		Topics:      g.Topics,
		URL:         g.HTMLURL,
		UpdatedAt:   g.UpdatedAt,
	}
	if g.License != nil {
		meta.License = g.License.SpdxID
	}
	return meta
}

func (c *Client) FetchRepository(ctx context.Context, owner, repo string) (*RepositoryData, error) {
	if !isValidSegment(owner) || !isValidSegment(repo) {
		return nil, errors.New("invalid owner or repository name")
	}

	// 1. メタ情報の取得
	metaPath := fmt.Sprintf("/repos/%s/%s", owner, repo)
	resp, err := c.sendRequest(ctx, "GET", metaPath, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, handleResponseError(resp)
	}

	var gMeta githubRepoMeta
	if err := json.NewDecoder(resp.Body).Decode(&gMeta); err != nil {
		return nil, err
	}

	// 2. 言語構成の取得
	langPath := fmt.Sprintf("/repos/%s/%s/languages", owner, repo)
	langResp, err := c.sendRequest(ctx, "GET", langPath, nil)
	if err != nil {
		return nil, err
	}
	defer langResp.Body.Close()

	if langResp.StatusCode != http.StatusOK {
		return nil, handleResponseError(langResp)
	}

	var languages map[string]int
	if err := json.NewDecoder(langResp.Body).Decode(&languages); err != nil {
		return nil, err
	}

	// 3. README の取得
	readmePath := fmt.Sprintf("/repos/%s/%s/readme", owner, repo)
	readmeResp, err := c.sendRequest(ctx, "GET", readmePath, map[string]string{
		"Accept": "application/vnd.github.raw",
	})
	if err != nil {
		return nil, err
	}
	defer readmeResp.Body.Close()

	if readmeResp.StatusCode != http.StatusOK {
		return nil, handleResponseError(readmeResp)
	}

	readmeBytes, err := io.ReadAll(io.LimitReader(readmeResp.Body, maxREADMEBytes))
	if err != nil {
		return nil, err
	}

	meta := repoMetaFromGitHub(gMeta)
	meta.Languages = languages

	return &RepositoryData{
		Meta:   meta,
		README: string(readmeBytes),
		Code:   c.fetchCodeContext(ctx, owner, repo, gMeta.DefaultBranch),
	}, nil
}

// FetchRepositoryMeta は /repos/{owner}/{repo} の1リクエストでメタ情報のみ返す。
func (c *Client) FetchRepositoryMeta(ctx context.Context, owner, repo string) (*core.RepoMeta, error) {
	if !isValidSegment(owner) || !isValidSegment(repo) {
		return nil, errors.New("invalid owner or repository name")
	}
	metaPath := fmt.Sprintf("/repos/%s/%s", owner, repo)
	resp, err := c.sendRequest(ctx, "GET", metaPath, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, handleResponseError(resp)
	}

	var gMeta githubRepoMeta
	if err := json.NewDecoder(resp.Body).Decode(&gMeta); err != nil {
		return nil, err
	}
	return repoMetaFromGitHub(gMeta), nil
}

// fetchCodeContext はツリーからファイルを選定して内容を取得する。失敗時は nil（フォールバック）。
func (c *Client) fetchCodeContext(ctx context.Context, owner, repo, defaultBranch string) *CodeContext {
	if defaultBranch == "" {
		return nil
	}
	treePath := fmt.Sprintf("/repos/%s/%s/git/trees/%s?recursive=1", owner, repo, defaultBranch)
	treeResp, err := c.sendRequest(ctx, "GET", treePath, nil)
	if err != nil {
		return nil
	}
	defer treeResp.Body.Close()
	if treeResp.StatusCode != http.StatusOK {
		return nil
	}
	treeBytes, err := io.ReadAll(io.LimitReader(treeResp.Body, maxTreeResponseBytes))
	if err != nil {
		return nil
	}
	var treeResponse struct {
		Tree []treeEntry `json:"tree"`
	}
	if err := json.Unmarshal(treeBytes, &treeResponse); err != nil {
		return nil
	}
	paths := selectCodeFiles(treeResponse.Tree)
	if len(paths) == 0 {
		return nil
	}
	files := make([]CodeFile, 0, len(paths))
	for _, path := range paths {
		content, ok := c.fetchFileContent(ctx, owner, repo, path)
		if !ok {
			continue
		}
		files = append(files, CodeFile{Path: path, Content: content})
	}
	if len(files) == 0 {
		return nil
	}
	return &CodeContext{Files: files}
}

func (c *Client) fetchFileContent(ctx context.Context, owner, repo, path string) (string, bool) {
	contentPath := fmt.Sprintf("/repos/%s/%s/contents/%s", owner, repo, path)
	resp, err := c.sendRequest(ctx, "GET", contentPath, map[string]string{
		"Accept": "application/vnd.github.raw",
	})
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCodeFileReadBytes))
	if err != nil {
		return "", false
	}
	return string(body), true
}

type HTTPError struct {
	StatusCode int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("github API returned status %d", e.StatusCode)
}

var _ GitHubClient = (*Client)(nil)
