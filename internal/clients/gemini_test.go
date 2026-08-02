package clients

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/issy20/reporepo/internal/core"
	"google.golang.org/genai"
)

type fakeGeminiGenerator struct {
	model    string
	contents []*genai.Content
	config   *genai.GenerateContentConfig
	response *genai.GenerateContentResponse
	err      error
	calls    int
}

func (f *fakeGeminiGenerator) GenerateContent(_ context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
	f.calls++
	f.model, f.contents, f.config = model, contents, config
	return f.response, f.err
}

func TestGeminiClientGenerateSendsPromptsAndParsesJSON(t *testing.T) {
	fake := &fakeGeminiGenerator{response: &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{
			Content: &genai.Content{Parts: []*genai.Part{{Text: `{"summary":"要約","tech_stack":"Go","background":"背景","keywords":["tui"]}`}}},
		}},
	}}
	c := &GeminiClient{apiKey: "secret", model: "gemini-test", generator: fake}
	got, err := c.Generate(context.Background(), &core.RepoMeta{FullName: "owner/repo"}, "readme", "ja")
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if fake.calls != 1 || fake.model != "gemini-test" || len(fake.contents) != 1 || fake.config == nil || fake.config.SystemInstruction == nil || fake.config.ResponseMIMEType != "application/json" {
		t.Fatalf("generator request = model:%q contents:%d config:%#v", fake.model, len(fake.contents), fake.config)
	}
	if got.Provider != "gemini" || got.Model != "gemini-test" || got.Summary != "要約" {
		t.Fatalf("analysis = %#v", got)
	}
}

func TestGeminiClientRejectsInvalidInputBeforeGenerator(t *testing.T) {
	fake := &fakeGeminiGenerator{}
	c := &GeminiClient{generator: fake}
	if _, err := c.Generate(context.Background(), nil, "", "ja"); err == nil || fake.calls != 0 {
		t.Fatalf("Generate(nil) error = %v, calls = %d", err, fake.calls)
	}
	if _, err := c.Generate(context.Background(), &core.RepoMeta{FullName: "owner/repo"}, "", "fr"); err == nil || fake.calls != 0 {
		t.Fatalf("Generate(fr) error = %v, calls = %d", err, fake.calls)
	}
}

func TestGeminiClientReturnsSafeResponseAndGeneratorErrors(t *testing.T) {
	const secret = "gemini-sensitive-key"
	tests := []struct {
		name     string
		response *genai.GenerateContentResponse
		err      error
	}{
		{name: "empty response", response: &genai.GenerateContentResponse{}},
		{name: "safety block", response: &genai.GenerateContentResponse{PromptFeedback: &genai.GenerateContentResponsePromptFeedback{BlockReason: genai.BlockedReasonSafety}}},
		{name: "generator error", err: errors.New("request rejected " + secret)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &GeminiClient{apiKey: secret, model: "model", generator: &fakeGeminiGenerator{response: tt.response, err: tt.err}}
			_, err := c.Generate(context.Background(), &core.RepoMeta{FullName: "owner/repo"}, "", "en")
			if err == nil || strings.Contains(err.Error(), secret) {
				t.Fatalf("Generate() error = %v", err)
			}
		})
	}
}

var _ AIClient = (*GeminiClient)(nil)
