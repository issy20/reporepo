package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/issy20/reporepo/internal/core"
	"github.com/issy20/reporepo/internal/presentation"
	"github.com/issy20/reporepo/internal/secretstore"
	"github.com/spf13/cobra"
)

// version はビルド時に -ldflags で上書きされる（GoReleaser の .Version 注入用）。
// 既定値は development ビルドでの表示に使われる。
var version = "0.1.0"

// Execute はプロセス引数を解釈して CLI を実行する。
func Execute() error {
	root := NewRootCommand()
	return executeRoot(root, func(out io.Writer) *presentation.Renderer {
		return presentation.NewRenderer(out, presentation.ResolveCapabilities(out))
	})
}

func executeRoot(root *cobra.Command, factory presenterFactory) error {
	err := root.Execute()
	if err == nil {
		return nil
	}
	// 描画に失敗しても、終了理由である元のerrorを維持する。
	renderer := factory(root.ErrOrStderr())
	_ = renderer.Error(err.Error())
	_ = renderer.Hint(fmt.Sprintf("%s --help で利用方法を確認できます。", root.Name()))
	return err
}

type presenterFactory func(io.Writer) *presentation.Renderer

type commandDependencies struct {
	run         func() error
	loadConfig  func() (*core.Config, error)
	saveConfig  func(*core.Config) error
	secretStore secretstore.Store
	configPath  func() (string, error)
	dataPath    func() (string, error)
	presenter   presenterFactory
	app         *applicationDependencies
}

// NewRootCommand は reporepo の CLI コマンドツリーを構築する。
func NewRootCommand() *cobra.Command {
	secrets := secretstore.NewKeyringStore()
	return newRootCommand(commandDependencies{
		run: runApplication, loadConfig: func() (*core.Config, error) {
			cfg, legacy, err := core.LoadConfigFile()
			if err != nil {
				return nil, err
			}
			if err := migrateLegacySecrets(cfg, legacy, secrets, core.SaveConfig); err != nil {
				return nil, err
			}
			return cfg, nil
		}, saveConfig: core.SaveConfig,
		secretStore: secrets,
		configPath:  core.ConfigFilePath, dataPath: dataFilePath,
		presenter: func(out io.Writer) *presentation.Renderer {
			return presentation.NewRenderer(out, presentation.ResolveCapabilities(out))
		},
	})
}

func newRootCommand(deps commandDependencies) *cobra.Command {
	if deps.run == nil {
		deps.run = func() error { return nil }
	}
	if deps.app == nil {
		app := defaultApplicationDependencies()
		deps.app = &app
	}
	if deps.presenter == nil {
		deps.presenter = func(out io.Writer) *presentation.Renderer {
			return presentation.NewRenderer(out, presentation.ResolveCapabilities(out))
		}
	}
	root := &cobra.Command{
		Use:           "reporepo",
		Short:         "GitHub リポジトリを AI で要約・解説する TUI",
		Long:          "reporepo は GitHub リポジトリを AI で要約・解説する TUI アプリケーションです。",
		Example:       "  reporepo run\n  reporepo config\n  reporepo where",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return deps.run()
		},
	}

	runCommand := &cobra.Command{Use: "run", Short: "TUI を起動する", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error { return deps.run() }}
	configCommand := &cobra.Command{Use: "config", Short: "設定を対話的に編集する", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return runConfigWizardStreams(cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), deps.presenter, deps.loadConfig, deps.saveConfig, deps.secretStore)
	}}
	versionCommand := &cobra.Command{Use: "version", Short: "バージョンを表示する", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		p := deps.presenter(cmd.OutOrStdout())
		if !p.Decorated() {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "reporepo %s\n", version)
			return err
		}
		if err := p.Title("reporepo"); err != nil {
			return err
		}
		return p.Summary([]presentation.Row{{Label: "version", Value: version}})
	}}
	whereCommand := &cobra.Command{Use: "where", Short: "設定とデータの保存先を表示する", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if deps.configPath == nil || deps.dataPath == nil {
			return fmt.Errorf("保存先の解決処理を利用できません")
		}
		configPath, err := deps.configPath()
		if err != nil {
			return fmt.Errorf("設定ファイルの保存先を解決できませんでした")
		}
		dataPath, err := deps.dataPath()
		if err != nil {
			return fmt.Errorf("データファイルの保存先を解決できませんでした")
		}
		p := deps.presenter(cmd.OutOrStdout())
		if !p.Decorated() {
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "config: %s\ndata: %s\n", configPath, dataPath)
			return err
		}
		if err := p.Section("保存先"); err != nil {
			return err
		}
		return p.Summary([]presentation.Row{{Label: "config", Value: configPath}, {Label: "data", Value: dataPath}})
	}}
	root.AddCommand(runCommand, configCommand, versionCommand, whereCommand, newAnalyzeCommand(deps), newTrendingCommand(deps))
	installHelp(root, deps.presenter)
	return root
}

func installHelp(root *cobra.Command, factory presenterFactory) {
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		p := factory(cmd.OutOrStdout())
		_ = p.Title(cmd.CommandPath())
		description := cmd.Long
		if description == "" {
			description = cmd.Short
		}
		if description != "" {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), description)
		}
		_ = p.Section("Usage")
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", cmd.UseLine())
		if commands := cmd.Commands(); len(commands) > 0 {
			_ = p.Section("Available Commands")
			for _, child := range commands {
				if child.IsAvailableCommand() {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %-12s %s\n", child.Name(), child.Short)
				}
			}
		}
		if cmd.HasAvailableLocalFlags() || cmd.HasAvailableInheritedFlags() {
			_ = p.Section("Flags")
			flags := strings.TrimRight(cmd.Flags().FlagUsages(), "\n")
			if flags != "" {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), flags)
			}
		}
		if cmd.Example != "" {
			_ = p.Section("Examples")
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), cmd.Example)
		}
		_ = p.Hint(fmt.Sprintf("%s [command] --help で詳細を表示します。", root.Name()))
	})
}
