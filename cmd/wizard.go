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
	"github.com/issy20/reporepo/internal/presentation"
	"github.com/issy20/reporepo/internal/secretstore"
	"golang.org/x/term"
)

var errWizardCanceled = errors.New("wizard canceled")

type secretAction uint8

const (
	keepSecret secretAction = iota
	setSecret
	deleteSecret
)

type secretEdit struct {
	action secretAction
	value  string
}

type secretSnapshot struct {
	value  string
	exists bool
}

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
	out       *presentation.Renderer
	errOut    *presentation.Renderer
	load      func() (*core.Config, error)
	save      func(*core.Config) error
	secrets   secretstore.Store
	lookupEnv func(string) (string, bool)
}

func runConfigWizard(in io.Reader, out io.Writer, load func() (*core.Config, error), save func(*core.Config) error, secrets secretstore.Store) error {
	return runConfigWizardStreams(in, out, out, func(w io.Writer) *presentation.Renderer {
		return presentation.NewRenderer(w, presentation.ResolveCapabilities(w))
	}, load, save, secrets)
}

func runConfigWizardStreams(in io.Reader, out, errOut io.Writer, factory presenterFactory, load func() (*core.Config, error), save func(*core.Config) error, secrets secretstore.Store) error {
	return runConfigWizardWith(wizardDependencies{
		io: newConsoleWizardIO(in, out), out: factory(out), errOut: factory(errOut), load: load, save: save, secrets: secrets, lookupEnv: os.LookupEnv,
	})
}

func runConfigWizardWith(deps wizardDependencies) error {
	if deps.io == nil || deps.load == nil || deps.save == nil || deps.secrets == nil {
		return errors.New("設定処理を利用できません")
	}
	if deps.lookupEnv == nil {
		deps.lookupEnv = func(string) (string, bool) { return "", false }
	}
	if deps.out == nil {
		deps.out = presentation.NewRenderer(io.Discard, presentation.Capabilities{})
	}
	if deps.errOut == nil {
		deps.errOut = presentation.NewRenderer(io.Discard, presentation.Capabilities{})
	}
	cfg, err := deps.load()
	if isMigrationFailure(err) {
		return err
	}
	if err != nil || cfg == nil {
		return errors.New("保存済み設定を読み込めませんでした")
	}
	updated := *cfg
	updated.GithubToken = ""
	updated.AnthropicAPIKey = ""
	updated.OpenAIAPIKey = ""
	updated.GeminiAPIKey = ""
	stored, err := loadWizardSecrets(deps.secrets)
	if err != nil {
		return errors.New("OS資格情報ストアから設定を読み込めませんでした")
	}
	if err := deps.out.Title("Reporepo configuration"); err != nil {
		return inputResult(err)
	}
	if err := deps.out.Section("Secrets"); err != nil {
		return inputResult(err)
	}
	if err := deps.out.Summary(secretRows(stored, deps.lookupEnv)); err != nil {
		return inputResult(err)
	}

	githubEdit, err := promptSecretEdit(deps.io, "GitHub token", stored[secretstore.GitHubToken].exists)
	if err != nil {
		return inputResult(err)
	}
	claudeEdit, err := promptSecretEdit(deps.io, "Anthropic API key", stored[secretstore.AnthropicAPIKey].exists)
	if err != nil {
		return inputResult(err)
	}
	openAIEdit, err := promptSecretEdit(deps.io, "OpenAI API key", stored[secretstore.OpenAIAPIKey].exists)
	if err != nil {
		return inputResult(err)
	}
	geminiEdit, err := promptSecretEdit(deps.io, "Gemini API key", stored[secretstore.GeminiAPIKey].exists)
	if err != nil {
		return inputResult(err)
	}

	githubEnv := envPresent(deps.lookupEnv, githubTokenEnv)
	claudeEnv := envPresent(deps.lookupEnv, anthropicAPIKeyEnv)
	openAIEnv := envPresent(deps.lookupEnv, openAIAPIKeyEnv)
	geminiEnv := envPresent(deps.lookupEnv, geminiAPIKeyEnv)
	if githubEnv {
		if err := deps.errOut.Warning("GITHUB_TOKEN が設定されているため、実行時は環境変数が優先されます。"); err != nil {
			return inputResult(err)
		}
	}
	if claudeEnv {
		if err := deps.errOut.Warning("ANTHROPIC_API_KEY が設定されているため、実行時は環境変数が優先されます。"); err != nil {
			return inputResult(err)
		}
	}
	if openAIEnv {
		if err := deps.errOut.Warning("OPENAI_API_KEY が設定されているため、実行時は環境変数が優先されます。"); err != nil {
			return inputResult(err)
		}
	}
	if geminiEnv {
		if err := deps.errOut.Warning("GEMINI_API_KEY が設定されているため、実行時は環境変数が優先されます。"); err != nil {
			return inputResult(err)
		}
	}

	for {
		fallback := updated.DefaultProvider
		if !validChoice(fallback, "claude", "openai", "gemini") {
			fallback = "claude"
		}
		updated.DefaultProvider, err = promptChoice(deps.io, deps.errOut.Warning, "既定provider", fallback, "claude", "openai", "gemini")
		if err != nil {
			return inputResult(err)
		}
		claudeConfigured := configuredAfterEdit(stored[secretstore.AnthropicAPIKey].exists, claudeEdit)
		openAIConfigured := configuredAfterEdit(stored[secretstore.OpenAIAPIKey].exists, openAIEdit)
		geminiConfigured := configuredAfterEdit(stored[secretstore.GeminiAPIKey].exists, geminiEdit)
		available := (updated.DefaultProvider == "claude" && (claudeConfigured || claudeEnv)) ||
			(updated.DefaultProvider == "openai" && (openAIConfigured || openAIEnv)) ||
			(updated.DefaultProvider == "gemini" && (geminiConfigured || geminiEnv))
		if available {
			break
		}
		if err := deps.errOut.Warning("選択したproviderのAPI keyがありません。secretを設定するか、利用可能なproviderを選択してください。"); err != nil {
			return inputResult(err)
		}
		if !claudeConfigured && !claudeEnv && !openAIConfigured && !openAIEnv && !geminiConfigured && !geminiEnv {
			if claudeEdit, err = promptSecretEdit(deps.io, "Anthropic API key", stored[secretstore.AnthropicAPIKey].exists); err != nil {
				return inputResult(err)
			}
			if openAIEdit, err = promptSecretEdit(deps.io, "OpenAI API key", stored[secretstore.OpenAIAPIKey].exists); err != nil {
				return inputResult(err)
			}
			if geminiEdit, err = promptSecretEdit(deps.io, "Gemini API key", stored[secretstore.GeminiAPIKey].exists); err != nil {
				return inputResult(err)
			}
		}
	}

	language := updated.DefaultLanguage
	if !validChoice(language, "ja", "en") {
		language = "ja"
	}
	updated.DefaultLanguage, err = promptChoice(deps.io, deps.errOut.Warning, "既定言語", language, "ja", "en")
	if err != nil {
		return inputResult(err)
	}

	configured := map[secretstore.Key]bool{
		secretstore.GitHubToken:     configuredAfterEdit(stored[secretstore.GitHubToken].exists, githubEdit),
		secretstore.AnthropicAPIKey: configuredAfterEdit(stored[secretstore.AnthropicAPIKey].exists, claudeEdit),
		secretstore.OpenAIAPIKey:    configuredAfterEdit(stored[secretstore.OpenAIAPIKey].exists, openAIEdit),
		secretstore.GeminiAPIKey:    configuredAfterEdit(stored[secretstore.GeminiAPIKey].exists, geminiEdit),
	}
	if err := deps.out.Section("Defaults"); err != nil {
		return inputResult(err)
	}
	if err := deps.out.Summary([]presentation.Row{{Label: "Provider", Value: updated.DefaultProvider}, {Label: "Language", Value: updated.DefaultLanguage}}); err != nil {
		return inputResult(err)
	}
	if err := deps.out.Section("Review"); err != nil {
		return inputResult(err)
	}
	if err := printSummary(deps.out, &updated, configured, githubEnv, claudeEnv, openAIEnv, geminiEnv); err != nil {
		return inputResult(err)
	}
	for {
		answer, readErr := deps.io.ReadLine("保存しますか? [y/N]: ")
		if readErr != nil {
			return inputResult(readErr)
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "y", "yes":
			edits := map[secretstore.Key]secretEdit{
				secretstore.GitHubToken: githubEdit, secretstore.AnthropicAPIKey: claudeEdit, secretstore.OpenAIAPIKey: openAIEdit, secretstore.GeminiAPIKey: geminiEdit,
			}
			if err := saveWizardChanges(deps.secrets, stored, edits, func() error { return deps.save(&updated) }); err != nil {
				return err
			}
			if err := deps.out.Success("設定を保存しました。"); err != nil {
				return inputResult(err)
			}
			return nil
		case "", "n", "no":
			if err := deps.out.Hint("設定の保存をキャンセルしました。"); err != nil {
				return inputResult(err)
			}
			return nil
		default:
			if err := deps.errOut.Warning("y または n を入力してください。"); err != nil {
				return inputResult(err)
			}
		}
	}
}

func promptSecretEdit(wio wizardIO, label string, configured bool) (secretEdit, error) {
	status := "未設定"
	if configured {
		status = "Keychain設定済み"
	}
	value, err := wio.ReadSecret(fmt.Sprintf("%s (%s、空欄で維持、-で削除): ", label, status))
	if err != nil {
		return secretEdit{}, err
	}
	value = strings.TrimSpace(value)
	switch value {
	case "":
		return secretEdit{action: keepSecret}, nil
	case "-":
		return secretEdit{action: deleteSecret}, nil
	default:
		return secretEdit{action: setSecret, value: value}, nil
	}
}

func loadWizardSecrets(store secretstore.Store) (map[secretstore.Key]secretSnapshot, error) {
	snapshots := make(map[secretstore.Key]secretSnapshot, 4)
	for _, key := range []secretstore.Key{secretstore.GitHubToken, secretstore.AnthropicAPIKey, secretstore.OpenAIAPIKey, secretstore.GeminiAPIKey} {
		value, err := store.Get(key)
		switch {
		case err == nil:
			snapshots[key] = secretSnapshot{value: value, exists: true}
		case errors.Is(err, secretstore.ErrNotFound):
			snapshots[key] = secretSnapshot{}
		default:
			return nil, err
		}
	}
	return snapshots, nil
}

func configuredAfterEdit(current bool, edit secretEdit) bool {
	switch edit.action {
	case setSecret:
		return true
	case deleteSecret:
		return false
	default:
		return current
	}
}

func saveWizardChanges(store secretstore.Store, snapshots map[secretstore.Key]secretSnapshot, edits map[secretstore.Key]secretEdit, saveConfig func() error) error {
	keys := []secretstore.Key{secretstore.GitHubToken, secretstore.AnthropicAPIKey, secretstore.OpenAIAPIKey, secretstore.GeminiAPIKey}
	applied := make([]secretstore.Key, 0, len(keys))
	for _, key := range keys {
		edit := edits[key]
		var err error
		switch edit.action {
		case setSecret:
			err = store.Set(key, edit.value)
		case deleteSecret:
			err = store.Delete(key)
		default:
			continue
		}
		if err != nil {
			return wizardSaveError(rollbackSecrets(store, snapshots, applied))
		}
		applied = append(applied, key)
	}
	if err := saveConfig(); err != nil {
		return wizardSaveError(rollbackSecrets(store, snapshots, applied))
	}
	return nil
}

func rollbackSecrets(store secretstore.Store, snapshots map[secretstore.Key]secretSnapshot, applied []secretstore.Key) bool {
	failed := false
	for i := len(applied) - 1; i >= 0; i-- {
		key := applied[i]
		snapshot := snapshots[key]
		var err error
		if snapshot.exists {
			err = store.Set(key, snapshot.value)
		} else {
			err = store.Delete(key)
		}
		if err != nil {
			failed = true
		}
	}
	return failed
}

func wizardSaveError(rollbackFailed bool) error {
	if rollbackFailed {
		return errors.New("設定の復元に失敗しました。OS資格情報ストアの設定を確認してください")
	}
	return errors.New("設定を保存できませんでした")
}

func promptChoice(wio wizardIO, report func(string) error, label, current string, choices ...string) (string, error) {
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
		if err := report(strings.Join(choices, ", ") + " のいずれかを入力してください。"); err != nil {
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
	return ok && strings.TrimSpace(value) != ""
}

func printSummary(out *presentation.Renderer, cfg *core.Config, configured map[secretstore.Key]bool, githubEnv, claudeEnv, openAIEnv, geminiEnv bool) error {
	return out.Summary([]presentation.Row{
		{Label: "GitHub token", Value: secretStatus(configured[secretstore.GitHubToken], githubEnv)},
		{Label: "Anthropic API key", Value: secretStatus(configured[secretstore.AnthropicAPIKey], claudeEnv)},
		{Label: "OpenAI API key", Value: secretStatus(configured[secretstore.OpenAIAPIKey], openAIEnv)},
		{Label: "Gemini API key", Value: secretStatus(configured[secretstore.GeminiAPIKey], geminiEnv)},
		{Label: "既定provider", Value: cfg.DefaultProvider}, {Label: "既定言語", Value: cfg.DefaultLanguage},
	})
}

func secretRows(stored map[secretstore.Key]secretSnapshot, lookup func(string) (string, bool)) []presentation.Row {
	return []presentation.Row{
		{Label: "GitHub token", Value: secretStatus(stored[secretstore.GitHubToken].exists, envPresent(lookup, githubTokenEnv))},
		{Label: "Anthropic API key", Value: secretStatus(stored[secretstore.AnthropicAPIKey].exists, envPresent(lookup, anthropicAPIKeyEnv))},
		{Label: "OpenAI API key", Value: secretStatus(stored[secretstore.OpenAIAPIKey].exists, envPresent(lookup, openAIAPIKeyEnv))},
		{Label: "Gemini API key", Value: secretStatus(stored[secretstore.GeminiAPIKey].exists, envPresent(lookup, geminiAPIKeyEnv))},
	}
}

func secretStatus(stored, environment bool) string {
	if stored && environment {
		return "Keychain設定済み（環境変数が優先）"
	}
	if environment {
		return "環境変数で設定済み"
	}
	if stored {
		return "Keychain設定済み"
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
