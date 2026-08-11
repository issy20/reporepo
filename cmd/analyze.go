package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/issy20/reporepo/internal/analyzer"
	"github.com/spf13/cobra"
)

func newAnalyzeCommand(deps commandDependencies) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analyze OWNER/REPO",
		Short: "リポジトリを解析して結果を出力する",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider, _ := cmd.Flags().GetString("provider")
			language, _ := cmd.Flags().GetString("language")
			jsonOutput, _ := cmd.Flags().GetBool("json")
			force, _ := cmd.Flags().GetBool("force")
			return runAnalyze(deps, args[0], provider, language, jsonOutput, force, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringP("provider", "p", "", "AI プロバイダ (claude, openai, gemini)")
	cmd.Flags().StringP("language", "l", "", "出力言語 (ja, en)")
	cmd.Flags().Bool("json", false, "JSON 形式で出力")
	cmd.Flags().BoolP("force", "f", false, "キャッシュを無視して再生成")
	return cmd
}

func runAnalyze(deps commandDependencies, input, provider, language string, jsonOutput, force bool, out, errOut io.Writer) error {
	if deps.app == nil {
		return errors.New("ランタイムを構築できません")
	}
	rt, err := buildRuntime(*deps.app, func(msg string) { fmt.Fprintln(errOut, "警告:", msg) }, true)
	if err != nil {
		return err
	}
	if provider == "" {
		provider = rt.cfg.DefaultProvider
	}
	if language == "" {
		language = rt.cfg.DefaultLanguage
	}
	if language != "ja" && language != "en" {
		return errors.New("language は ja か en を指定してください")
	}
	if _, ok := rt.ai[provider]; !ok {
		return errors.New("API key が設定されていません。`reporepo config` で設定できます")
	}
	a := analyzer.New(rt.store, rt.github, rt.ai, time.Now, analyzer.DefaultRefreshInterval)
	result, err := a.Analyze(context.Background(), input, language, provider, force)
	if err != nil {
		return err
	}
	for _, warning := range result.Warnings {
		fmt.Fprintln(errOut, "警告:", warning)
	}
	return writeAnalyzeOutput(out, result, language, jsonOutput)
}

func writeAnalyzeOutput(out io.Writer, result *analyzer.Result, language string, jsonOutput bool) error {
	if jsonOutput {
		return writeAnalyzeJSON(out, result, language)
	}
	_, err := fmt.Fprintln(out, formatAnalyzePlain(result, language, time.Now()))
	return err
}
