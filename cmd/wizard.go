package cmd

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/issy20/reporepo/internal/core"
)

func runConfigWizard(in io.Reader, out io.Writer, load func() (*core.Config, error), save func(*core.Config) error) error {
	if load == nil || save == nil {
		return fmt.Errorf("設定処理を利用できません")
	}
	cfg, err := load()
	if err != nil {
		return fmt.Errorf("設定を読み込めません: %w", err)
	}
	if cfg == nil {
		return fmt.Errorf("設定を読み込めません: 結果が空です")
	}
	updated := *cfg
	reader := bufio.NewReader(in)

	if updated.GithubToken, err = readSecret(reader, out, "GitHub token", updated.GithubToken); err != nil {
		return err
	}
	if updated.AnthropicAPIKey, err = readSecret(reader, out, "Anthropic API key", updated.AnthropicAPIKey); err != nil {
		return err
	}
	if updated.OpenAIAPIKey, err = readSecret(reader, out, "OpenAI API key", updated.OpenAIAPIKey); err != nil {
		return err
	}
	if updated.DefaultProvider, err = readChoice(reader, out, "Default provider", updated.DefaultProvider, "claude", "openai"); err != nil {
		return err
	}
	if updated.DefaultLanguage, err = readChoice(reader, out, "Default language", updated.DefaultLanguage, "ja", "en"); err != nil {
		return err
	}

	if err := save(&updated); err != nil {
		return fmt.Errorf("設定を保存できません: %w", err)
	}
	_, err = fmt.Fprintln(out, "設定を保存しました。")
	return err
}

func readSecret(reader *bufio.Reader, out io.Writer, label, current string) (string, error) {
	if _, err := fmt.Fprintf(out, "%s (空欄で現在値を維持): ", label); err != nil {
		return "", err
	}
	value, err := readLine(reader)
	if err != nil {
		return "", err
	}
	if value == "" {
		return current, nil
	}
	return value, nil
}

func readChoice(reader *bufio.Reader, out io.Writer, label, current string, choices ...string) (string, error) {
	for {
		if _, err := fmt.Fprintf(out, "%s [%s] (%s): ", label, current, strings.Join(choices, "/")); err != nil {
			return "", err
		}
		value, err := readLine(reader)
		if err != nil {
			return "", err
		}
		if value == "" {
			return current, nil
		}
		for _, choice := range choices {
			if value == choice {
				return value, nil
			}
		}
		if _, err := fmt.Fprintf(out, "%s のいずれかを入力してください。\n", strings.Join(choices, ", ")); err != nil {
			return "", err
		}
	}
}

func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && !(err == io.EOF && len(line) > 0) {
		return "", fmt.Errorf("入力を読み取れません: %w", err)
	}
	return strings.TrimSpace(line), nil
}
