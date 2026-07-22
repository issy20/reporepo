package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/issy20/reporepo/internal/core"
	"golang.org/x/term"
)

var errWizardCanceled = errors.New("wizard canceled")

type wizardIO interface {
	ReadLine(prompt string) (string, error)
	ReadSecret(prompt string) (string, error)
	Println(message string) error
}

type consoleWizardIO struct {
	reader       *bufio.Reader
	in           io.Reader
	out          io.Writer
	isTerminal   func(int) bool
	readPassword func(int) ([]byte, error)
}

func newConsoleWizardIO(in io.Reader, out io.Writer) wizardIO {
	return &consoleWizardIO{
		reader: bufio.NewReader(in), in: in, out: out,
		isTerminal: term.IsTerminal, readPassword: term.ReadPassword,
	}
}

func (c *consoleWizardIO) ReadLine(prompt string) (string, error) {
	if _, err := fmt.Fprint(c.out, prompt); err != nil {
		return "", err
	}
	return readLine(c.reader)
}

func (c *consoleWizardIO) ReadSecret(prompt string) (string, error) {
	if _, err := fmt.Fprint(c.out, prompt); err != nil {
		return "", err
	}
	if file, ok := c.in.(*os.File); ok && c.isTerminal(int(file.Fd())) {
		value, err := c.readPassword(int(file.Fd()))
		if _, writeErr := fmt.Fprintln(c.out); err == nil && writeErr != nil {
			err = writeErr
		}
		return strings.TrimSpace(string(value)), err
	}
	return readLine(c.reader)
}

func (c *consoleWizardIO) Println(message string) error {
	_, err := fmt.Fprintln(c.out, message)
	return err
}

type wizardDependencies struct {
	io        wizardIO
	load      func() (*core.Config, error)
	save      func(*core.Config) error
	lookupEnv func(string) (string, bool)
}

func runConfigWizard(in io.Reader, out io.Writer, load func() (*core.Config, error), save func(*core.Config) error) error {
	return runConfigWizardWith(wizardDependencies{
		io: newConsoleWizardIO(in, out), load: load, save: save, lookupEnv: os.LookupEnv,
	})
}

func runConfigWizardWith(deps wizardDependencies) error {
	if deps.io == nil || deps.load == nil || deps.save == nil {
		return errors.New("設定処理を利用できません")
	}
	if deps.lookupEnv == nil {
		deps.lookupEnv = func(string) (string, bool) { return "", false }
	}
	cfg, err := deps.load()
	if err != nil || cfg == nil {
		return errors.New("保存済み設定を読み込めませんでした")
	}
	updated := *cfg

	if updated.GithubToken, err = promptSecret(deps.io, "GitHub token", updated.GithubToken); err != nil {
		return inputResult(err)
	}
	if updated.AnthropicAPIKey, err = promptSecret(deps.io, "Anthropic API key", updated.AnthropicAPIKey); err != nil {
		return inputResult(err)
	}
	if updated.OpenAIAPIKey, err = promptSecret(deps.io, "OpenAI API key", updated.OpenAIAPIKey); err != nil {
		return inputResult(err)
	}

	githubEnv := envPresent(deps.lookupEnv, "GITHUB_TOKEN")
	claudeEnv := envPresent(deps.lookupEnv, "ANTHROPIC_API_KEY")
	openAIEnv := envPresent(deps.lookupEnv, "OPENAI_API_KEY")
	if githubEnv {
		if err := deps.io.Println("GITHUB_TOKEN が設定されているため、実行時は環境変数が優先されます。"); err != nil {
			return inputResult(err)
		}
	}
	if claudeEnv {
		if err := deps.io.Println("ANTHROPIC_API_KEY が設定されているため、実行時は環境変数が優先されます。"); err != nil {
			return inputResult(err)
		}
	}
	if openAIEnv {
		if err := deps.io.Println("OPENAI_API_KEY が設定されているため、実行時は環境変数が優先されます。"); err != nil {
			return inputResult(err)
		}
	}

	for {
		fallback := updated.DefaultProvider
		if !validChoice(fallback, "claude", "openai") {
			fallback = "claude"
		}
		updated.DefaultProvider, err = promptChoice(deps.io, "既定provider", fallback, "claude", "openai")
		if err != nil {
			return inputResult(err)
		}
		available := (updated.DefaultProvider == "claude" && (updated.AnthropicAPIKey != "" || claudeEnv)) ||
			(updated.DefaultProvider == "openai" && (updated.OpenAIAPIKey != "" || openAIEnv))
		if available {
			break
		}
		if err := deps.io.Println("選択したproviderのAPI keyがありません。secretを設定するか、利用可能なproviderを選択してください。"); err != nil {
			return inputResult(err)
		}
		if updated.AnthropicAPIKey == "" && !claudeEnv && updated.OpenAIAPIKey == "" && !openAIEnv {
			if updated.AnthropicAPIKey, err = promptSecret(deps.io, "Anthropic API key", updated.AnthropicAPIKey); err != nil {
				return inputResult(err)
			}
			if updated.OpenAIAPIKey, err = promptSecret(deps.io, "OpenAI API key", updated.OpenAIAPIKey); err != nil {
				return inputResult(err)
			}
		}
	}

	language := updated.DefaultLanguage
	if !validChoice(language, "ja", "en") {
		language = "ja"
	}
	updated.DefaultLanguage, err = promptChoice(deps.io, "既定言語", language, "ja", "en")
	if err != nil {
		return inputResult(err)
	}

	if err := printSummary(deps.io, &updated, githubEnv, claudeEnv, openAIEnv); err != nil {
		return inputResult(err)
	}
	for {
		answer, readErr := deps.io.ReadLine("保存しますか? [y/N]: ")
		if readErr != nil {
			return inputResult(readErr)
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "y", "yes":
			if err := deps.save(&updated); err != nil {
				return errors.New("設定を保存できませんでした")
			}
			if err := deps.io.Println("設定を保存しました。"); err != nil {
				return inputResult(err)
			}
			return nil
		case "", "n", "no":
			if err := deps.io.Println("設定の保存をキャンセルしました。"); err != nil {
				return inputResult(err)
			}
			return nil
		default:
			if err := deps.io.Println("y または n を入力してください。"); err != nil {
				return inputResult(err)
			}
		}
	}
}

func promptSecret(wio wizardIO, label, current string) (string, error) {
	status := "未設定"
	if current != "" {
		status = "設定済み"
	}
	value, err := wio.ReadSecret(fmt.Sprintf("%s (%s、空欄で維持、-で削除): ", label, status))
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return current, nil
	}
	if value == "-" {
		return "", nil
	}
	return value, nil
}

func promptChoice(wio wizardIO, label, current string, choices ...string) (string, error) {
	for {
		value, err := wio.ReadLine(fmt.Sprintf("%s [%s] (%s): ", label, current, strings.Join(choices, "/")))
		if err != nil {
			return "", err
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return current, nil
		}
		if validChoice(value, choices...) {
			return value, nil
		}
		if err := wio.Println(strings.Join(choices, ", ") + " のいずれかを入力してください。"); err != nil {
			return "", err
		}
	}
}

func validChoice(value string, choices ...string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}

func envPresent(lookup func(string) (string, bool), name string) bool {
	value, ok := lookup(name)
	return ok && value != ""
}

func printSummary(wio wizardIO, cfg *core.Config, githubEnv, claudeEnv, openAIEnv bool) error {
	lines := []string{
		"GitHub token: " + secretStatus(cfg.GithubToken != "", githubEnv),
		"Anthropic API key: " + secretStatus(cfg.AnthropicAPIKey != "", claudeEnv),
		"OpenAI API key: " + secretStatus(cfg.OpenAIAPIKey != "", openAIEnv),
		"既定provider: " + cfg.DefaultProvider,
		"既定言語: " + cfg.DefaultLanguage,
	}
	for _, line := range lines {
		if err := wio.Println(line); err != nil {
			return err
		}
	}
	return nil
}

func secretStatus(stored, environment bool) string {
	if stored && environment {
		return "ファイル設定済み（環境変数が優先）"
	}
	if environment {
		return "環境変数で設定済み"
	}
	if stored {
		return "ファイル設定済み"
	}
	return "未設定"
}

func inputResult(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, errWizardCanceled) || errors.Is(err, syscall.EINTR) {
		return nil
	}
	return errors.New("設定入力を続行できませんでした")
}

func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) && len(line) > 0 {
			return strings.TrimSpace(line), nil
		}
		return "", err
	}
	return strings.TrimSpace(strings.TrimSuffix(line, "\r")), nil
}
