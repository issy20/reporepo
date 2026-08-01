package cmd

import (
	"fmt"

	"github.com/issy20/reporepo/internal/core"
	"github.com/issy20/reporepo/internal/secretstore"
	"github.com/spf13/cobra"
)

const version = "0.1.0"

// Execute はプロセス引数を解釈して CLI を実行する。
func Execute() error {
	return NewRootCommand().Execute()
}

type commandDependencies struct {
	run         func() error
	loadConfig  func() (*core.Config, error)
	saveConfig  func(*core.Config) error
	secretStore secretstore.Store
	configPath  func() (string, error)
	dataPath    func() (string, error)
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
	})
}

func newRootCommand(deps commandDependencies) *cobra.Command {
	if deps.run == nil {
		deps.run = func() error { return nil }
	}
	root := &cobra.Command{
		Use:          "reporepo",
		Short:        "GitHub リポジトリを AI で要約・解説する TUI",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return deps.run()
		},
	}

	runCommand := &cobra.Command{Use: "run", Short: "TUI を起動する", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error { return deps.run() }}
	configCommand := &cobra.Command{Use: "config", Short: "設定を対話的に編集する", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return runConfigWizard(cmd.InOrStdin(), cmd.OutOrStdout(), deps.loadConfig, deps.saveConfig, deps.secretStore)
	}}
	versionCommand := &cobra.Command{Use: "version", Short: "バージョンを表示する", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "reporepo %s\n", version)
		return err
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
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "config: %s\ndata: %s\n", configPath, dataPath)
		return err
	}}
	root.AddCommand(runCommand, configCommand, versionCommand, whereCommand)
	return root
}
