package clients

import (
	"net/http"
	"strings"
	"testing"

	"github.com/yourname/reporepo/internal/core"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBuildPrompts_IncludesRepositoryAndLanguageAndTruncatesREADMEByRune(t *testing.T) {
	readme := strings.Repeat("界", maxREADMECharacters+10)
	meta := &core.RepoMeta{FullName: "owner/repo", Description: "desc", Stars: 42, Language: "Go", Languages: map[string]int{"Go": 100}}

	system, user, err := buildPrompts(meta, readme, "ja")
	if err != nil {
		t.Fatalf("buildPrompts: %v", err)
	}
	if !strings.Contains(system, "日本語") || !strings.Contains(system, "summary") || !strings.Contains(system, "tech_stack") || !strings.Contains(system, "background") || !strings.Contains(system, "keywords") {
		t.Errorf("system prompt must prescribe Japanese and the JSON schema: %q", system)
	}
	if !strings.Contains(user, "owner/repo") || !strings.Contains(user, "42") || !strings.Contains(user, "Go") {
		t.Errorf("user prompt lacks repository metadata: %q", user)
	}
	if got := strings.Count(user, "界"); got != maxREADMECharacters {
		t.Errorf("README rune count = %d, want %d", got, maxREADMECharacters)
	}
}

func TestBuildPrompts_RejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		meta *core.RepoMeta
		lang string
	}{
		{name: "nil metadata", lang: "ja"},
		{name: "empty full name", meta: &core.RepoMeta{}, lang: "ja"},
		{name: "unsupported language", meta: &core.RepoMeta{FullName: "o/r"}, lang: "fr"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := buildPrompts(tt.meta, "readme", tt.lang); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestParseAnalysis_ExtractsJSONObjectAndSetsMetadata(t *testing.T) {
	raw := "preface\n```json\n{\"summary\":\"sum\",\"tech_stack\":\"stack\",\"background\":\"bg\",\"keywords\":[\"go\"]}\n```\nafter"
	got, err := parseAnalysis(raw, "en", "openai", "test-model")
	if err != nil {
		t.Fatalf("parseAnalysis: %v", err)
	}
	if got.Summary != "sum" || got.TechStack != "stack" || got.Background != "bg" || len(got.Keywords) != 1 {
		t.Fatalf("unexpected analysis: %#v", got)
	}
	if got.Language != "en" || got.Provider != "openai" || got.Model != "test-model" || got.CreatedAt.IsZero() {
		t.Errorf("generation metadata not populated: %#v", got)
	}
}

func TestParseAnalysis_RejectsMissingOrMalformedJSON(t *testing.T) {
	for _, raw := range []string{"plain text", "prefix {broken} suffix", `{\"summary\":\"only\"}`} {
		if _, err := parseAnalysis(raw, "ja", "claude", "model"); err == nil {
			t.Errorf("parseAnalysis(%q) should fail", raw)
		}
	}
}
