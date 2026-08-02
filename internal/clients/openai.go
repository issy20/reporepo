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

const defaultOpenAIEndpoint = "https://api.openai.com/v1/chat/completions"

// OpenAIClient は OpenAI Chat Completions API の AIClient 実装。
type OpenAIClient struct {
	apiKey   string
	model    string
	endpoint string
	http     *http.Client
}

func NewOpenAIClient(apiKey, model string, httpClient *http.Client) *OpenAIClient {
	return &OpenAIClient{apiKey: apiKey, model: model, endpoint: defaultOpenAIEndpoint, http: httpClient}
}

func (c *OpenAIClient) ProviderModel() (string, string) { return "openai", c.model }

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponseFormat struct {
	Type string `json:"type"`
}

type openAIRequest struct {
	Model          string               `json:"model"`
	Messages       []openAIMessage      `json:"messages"`
	ResponseFormat openAIResponseFormat `json:"response_format"`
}

type openAIChoice struct {
	Message openAIMessage `json:"message"`
}

type openAIResponse struct {
	Choices []openAIChoice `json:"choices"`
}

type openAIErrorResponse struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *OpenAIClient) Generate(ctx context.Context, meta *core.RepoMeta, readme, language string) (*core.Analysis, error) {
	system, user, err := buildPrompts(meta, readme, language)
	if err != nil {
		return nil, err
	}

	reqBody := openAIRequest{
		Model: c.model,
		Messages: []openAIMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		ResponseFormat: openAIResponseFormat{
			Type: "json_object",
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
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
		var errResp openAIErrorResponse
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
		return nil, fmt.Errorf("openai API error (status %d): %s", resp.StatusCode, errMsg)
	}

	var res openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(res.Choices) == 0 {
		return nil, fmt.Errorf("empty choices in openai response")
	}

	rawJSON := res.Choices[0].Message.Content
	return parseAnalysis(rawJSON, language, "openai", c.model)
}
