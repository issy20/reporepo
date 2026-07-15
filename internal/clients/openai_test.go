package clients

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/issy20/reporepo/internal/core"
)

func TestOpenAIGenerate_SendsChatCompletionJSONModeAndMapsResponse(t *testing.T) {
	var request map[string]any
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("unexpected request: method=%s auth=%q", r.Method, r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		body := `{"choices":[{"message":{"content":"{\"summary\":\"s\",\"tech_stack\":\"t\",\"background\":\"b\",\"keywords\":[\"k\"]}"}}]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	client := NewOpenAIClient("secret", "gpt-test", httpClient)
	got, err := client.Generate(context.Background(), &core.RepoMeta{FullName: "o/r"}, "README", "en")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	format, _ := request["response_format"].(map[string]any)
	if request["model"] != "gpt-test" || request["messages"] == nil || format["type"] != "json_object" {
		t.Errorf("invalid Chat Completions body: %#v", request)
	}
	if got.Provider != "openai" || got.Model != "gpt-test" || got.Summary != "s" {
		t.Errorf("unexpected analysis: %#v", got)
	}
}

func TestOpenAIGenerate_ReportsEmptyChoicesAndNon2xx(t *testing.T) {
	tests := []struct {
		name, body string
		status     int
	}{
		{name: "empty choices", body: `{"choices":[]}`, status: http.StatusOK},
		{name: "api error", body: `{"error":{"message":"denied"}}`, status: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tt.status, Body: io.NopCloser(strings.NewReader(tt.body)), Header: make(http.Header)}, nil
			})}
			client := NewOpenAIClient("secret", "model", httpClient)
			if _, err := client.Generate(context.Background(), &core.RepoMeta{FullName: "o/r"}, "", "ja"); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestOpenAIGenerate_Non2xxDoesNotLeakAPIKey(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		body := `{"error":{"message":"invalid key: top-secret"}}`
		return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	client := NewOpenAIClient("top-secret", "model", httpClient)

	_, err := client.Generate(context.Background(), &core.RepoMeta{FullName: "o/r"}, "", "ja")
	if err == nil {
		t.Fatal("expected API error")
	}
	if strings.Contains(err.Error(), "top-secret") {
		t.Fatalf("API key leaked in error: %v", err)
	}
}
