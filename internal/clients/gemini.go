package clients

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/issy20/reporepo/internal/core"
	"google.golang.org/genai"
)

type geminiGenerator interface {
	GenerateContent(context.Context, string, []*genai.Content, *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error)
}

// GeminiClient は Gemini Developer API の AIClient 実装。
type GeminiClient struct {
	apiKey    string
	model     string
	generator geminiGenerator
}

func NewGeminiClient(apiKey, model string) (*GeminiClient, error) {
	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey: apiKey, Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, errors.New("Gemini clientを初期化できませんでした")
	}
	return &GeminiClient{apiKey: apiKey, model: model, generator: client.Models}, nil
}

func (c *GeminiClient) ProviderModel() (string, string) { return "gemini", c.model }

func (c *GeminiClient) Generate(ctx context.Context, meta *core.RepoMeta, readme, code, language string) (*core.Analysis, error) {
	system, user, err := buildPrompts(meta, readme, code, language)
	if err != nil {
		return nil, err
	}
	if c.generator == nil {
		return nil, errors.New("Gemini clientを利用できません")
	}
	response, err := c.generator.GenerateContent(ctx, c.model, genai.Text(user), &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(system, "system"),
		ResponseMIMEType:  "application/json",
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, errors.New("Gemini APIへのリクエストに失敗しました")
	}
	if response == nil {
		return nil, errors.New("Gemini APIから応答を受け取れませんでした")
	}
	if response.PromptFeedback != nil && response.PromptFeedback.BlockReason != "" {
		return nil, errors.New("Gemini APIが安全性の理由で応答を拒否しました")
	}
	raw := strings.TrimSpace(response.Text())
	if raw == "" {
		return nil, fmt.Errorf("Gemini APIの応答に解析可能なテキストがありません")
	}
	return parseAnalysis(raw, language, "gemini", c.model)
}
