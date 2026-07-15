package clients

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/yourname/reporepo/internal/core"
)

func TestClaudeGenerate_SendsMessagesRequestAndMapsResponse(t *testing.T) {
	var request map[string]any
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.Header.Get("x-api-key") != "secret" || r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("unexpected request: method=%s headers=%v", r.Method, r.Header)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		body := `{"content":[{"type":"text","text":"{\"summary\":\"s\",\"tech_stack\":\"t\",\"background\":\"b\",\"keywords\":[\"k\"]}"}]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	client := NewClaudeClient("secret", "claude-test", httpClient)
	got, err := client.Generate(context.Background(), &core.RepoMeta{FullName: "o/r"}, "README", "ja")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if request["model"] != "claude-test" || request["system"] == nil || request["messages"] == nil || request["max_tokens"] == nil {
		t.Errorf("invalid Messages API body: %#v", request)
	}
	if got.Provider != "claude" || got.Model != "claude-test" || got.Summary != "s" {
		t.Errorf("unexpected analysis: %#v", got)
	}
}

func TestClaudeGenerate_ReportsNon2xxWithoutLeakingSecret(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"invalid key: top-secret"}}`)), Header: make(http.Header)}, nil
	})}
	client := NewClaudeClient("top-secret", "model", httpClient)
	_, err := client.Generate(context.Background(), &core.RepoMeta{FullName: "o/r"}, "", "en")
	if err == nil {
		t.Fatal("expected API error")
	}
	if strings.Contains(err.Error(), "top-secret") {
		t.Fatalf("API key leaked in error: %v", err)
	}
}
