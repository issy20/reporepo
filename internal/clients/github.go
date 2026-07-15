package clients

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/issy20/reporepo/internal/core"
)

var (
	ErrNotFound    = errors.New("repository not found")
	ErrRateLimited = errors.New("github rate limit exceeded")
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
}

type GitHubClient interface {
	FetchRepository(ctx context.Context, owner, repo string) (*RepositoryData, error)
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
	UpdatedAt time.Time `json:"updated_at"`
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

	readmeBytes, err := io.ReadAll(readmeResp.Body)
	if err != nil {
		return nil, err
	}

	meta := &core.RepoMeta{
		FullName:    gMeta.FullName,
		Description: gMeta.Description,
		Stars:       gMeta.StargazersCount,
		Forks:       gMeta.ForksCount,
		Language:    gMeta.Language,
		Topics:      gMeta.Topics,
		Languages:   languages,
		URL:         gMeta.HTMLURL,
		UpdatedAt:   gMeta.UpdatedAt,
	}
	if gMeta.License != nil {
		meta.License = gMeta.License.SpdxID
	}

	return &RepositoryData{
		Meta:   meta,
		README: string(readmeBytes),
	}, nil
}

type HTTPError struct {
	StatusCode int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("github API returned status %d", e.StatusCode)
}

var _ GitHubClient = (*Client)(nil)
