package clients

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yourname/reporepo/internal/core"
)

const maxREADMECharacters = 12_000

// ErrNotImplemented は Path A の契約を green 実装へ引き渡すための一時エラー。
var ErrNotImplemented = errors.New("AI client is not implemented")

// AIClient は TUI から AI プロバイダの差異を隠す境界。
type AIClient interface {
	Generate(ctx context.Context, meta *core.RepoMeta, readme, language string) (*core.Analysis, error)
}

func buildPrompts(meta *core.RepoMeta, readme, language string) (system, user string, err error) {
	if meta == nil {
		return "", "", errors.New("metadata is nil")
	}
	if meta.FullName == "" {
		return "", "", errors.New("empty repository full name")
	}
	if language != "ja" && language != "en" {
		return "", "", fmt.Errorf("unsupported language: %s", language)
	}

	runes := []rune(readme)
	if len(runes) > maxREADMECharacters {
		runes = runes[:maxREADMECharacters]
	}
	truncatedReadme := string(runes)

	var systemLang string
	if language == "ja" {
		systemLang = "日本語"
	} else {
		systemLang = "English"
	}

	system = fmt.Sprintf("You must analyze the repository and output the result in %s. The output MUST be a valid JSON object matching this schema exactly:\n{\n  \"summary\": \"string\",\n  \"tech_stack\": \"string\",\n  \"background\": \"string\",\n  \"keywords\": [\"string\"]\n}", systemLang)

	user = fmt.Sprintf("Repository: %s\nStars: %d\nLanguage: %s\nDescription: %s\nREADME:\n%s",
		meta.FullName, meta.Stars, meta.Language, meta.Description, truncatedReadme)

	return system, user, nil
}

type analysisJSON struct {
	Summary    string   `json:"summary"`
	TechStack  string   `json:"tech_stack"`
	Background string   `json:"background"`
	Keywords   []string `json:"keywords"`
}

func parseAnalysis(raw, language, provider, model string) (*core.Analysis, error) {
	var jsonStr string

	if idx := strings.Index(raw, "```json"); idx != -1 {
		start := idx + len("```json")
		if end := strings.Index(raw[start:], "```"); end != -1 {
			jsonStr = raw[start : start+end]
		}
	}

	if jsonStr == "" {
		start := strings.Index(raw, "{")
		end := strings.LastIndex(raw, "}")
		if start != -1 && end != -1 && start < end {
			jsonStr = raw[start : end+1]
		}
	}

	if jsonStr == "" {
		return nil, errors.New("no JSON object found")
	}

	var aj analysisJSON
	if err := json.Unmarshal([]byte(jsonStr), &aj); err != nil {
		return nil, err
	}

	if aj.Summary == "" || aj.TechStack == "" || aj.Background == "" || aj.Keywords == nil {
		return nil, errors.New("missing required fields in JSON")
	}

	return &core.Analysis{
		Summary:    aj.Summary,
		TechStack:  aj.TechStack,
		Background: aj.Background,
		Keywords:   aj.Keywords,
		Language:   language,
		Provider:   provider,
		Model:      model,
		CreatedAt:  time.Now(),
	}, nil
}
