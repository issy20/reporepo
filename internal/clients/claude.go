package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/issy20/reporepo/internal/core"
)

const defaultClaudeEndpoint = "https://api.anthropic.com/v1/messages"

// ClaudeClient は Anthropic Messages API の AIClient 実装。
type ClaudeClient struct {
	apiKey   string
	model    string
	endpoint string
	http     *http.Client
}

func NewClaudeClient(apiKey, model string, httpClient *http.Client) *ClaudeClient {
	return &ClaudeClient{apiKey: apiKey, model: model, endpoint: defaultClaudeEndpoint, http: httpClient}
}

func (c *ClaudeClient) ProviderModel() (string, string) { return "claude", c.model }

type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type claudeRequest struct {
	Model     string          `json:"model"`
	System    string          `json:"system"`
	Messages  []claudeMessage `json:"messages"`
	MaxTokens int             `json:"max_tokens"`
}

type claudeContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type claudeResponse struct {
	Content []claudeContent `json:"content"`
}

type claudeErrorResponse struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *ClaudeClient) Generate(ctx context.Context, meta *core.RepoMeta, readme, language string) (*core.Analysis, error) {
	system, user, err := buildPrompts(meta, readme, language)
	if err != nil {
		return nil, err
	}

	reqBody := claudeRequest{
		Model:  c.model,
		System: system,
		Messages: []claudeMessage{
			{Role: "user", Content: user},
		},
		MaxTokens: 4000,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}

	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	client := c.http
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errResp claudeErrorResponse
		respBytes, errRead := io.ReadAll(resp.Body)
		if errRead == nil {
			_ = json.Unmarshal(respBytes, &errResp)
		}
		errMsg := errResp.Error.Message
		if errMsg == "" {
			if len(respBytes) > 0 {
				errMsg = string(respBytes)
			} else {
				errMsg = resp.Status
			}
		}
		if c.apiKey != "" {
			errMsg = strings.ReplaceAll(errMsg, c.apiKey, "REDACTED")
		}
		return nil, fmt.Errorf("claude API error (status %d): %s", resp.StatusCode, errMsg)
	}

	var res claudeResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(res.Content) == 0 {
		return nil, fmt.Errorf("empty content in claude response")
	}

	var rawJSON string
	for _, cnt := range res.Content {
		if cnt.Type == "text" {
			rawJSON = cnt.Text
			break
		}
	}
	if rawJSON == "" {
		return nil, fmt.Errorf("no text content found in claude response")
	}

	return parseAnalysis(rawJSON, language, "claude", c.model)
}
