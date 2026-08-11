package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/issy20/reporepo/internal/clients"
	"github.com/issy20/reporepo/internal/trendingcache"
	"github.com/spf13/cobra"
)

func newTrendingCommand(deps commandDependencies) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trending",
		Short: "直近に作成されスターが伸びたリポジトリの一覧を表示する",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			since, _ := cmd.Flags().GetString("since")
			language, _ := cmd.Flags().GetString("language")
			minStars, _ := cmd.Flags().GetInt("min-stars")
			jsonOutput, _ := cmd.Flags().GetBool("json")
			return runTrending(deps, since, language, minStars, jsonOutput, time.Now, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().String("since", "week", "作成日時ウィンドウ (today, week, month)")
	cmd.Flags().String("language", "", "言語絞り込み")
	cmd.Flags().Int("min-stars", 50, "スター数の下限")
	cmd.Flags().Bool("json", false, "JSON 形式で出力")
	return cmd
}

// sinceToCreatedAfter は since 文字列を検索対象の作成日時下限へ変換する。
func sinceToCreatedAfter(since string, now time.Time) (time.Time, error) {
	switch since {
	case "today":
		return now.AddDate(0, 0, -1), nil
	case "week":
		return now.AddDate(0, 0, -7), nil
	case "month":
		return now.AddDate(0, 0, -30), nil
	}
	return time.Time{}, errors.New("since は today、week、month のいずれかを指定してください")
}

// runTrending は疑似Trending一覧を取得して出力する。AI キーは不要。
func runTrending(deps commandDependencies, since, language string, minStars int, jsonOutput bool, now func() time.Time, out, errOut io.Writer) error {
	if deps.app == nil {
		return errors.New("ランタイムを構築できません")
	}
	rt, err := buildRuntime(*deps.app, func(msg string) { fmt.Fprintln(errOut, "警告:", msg) }, false)
	if err != nil {
		return err
	}
	nowValue := now()
	createdAfter, err := sinceToCreatedAfter(since, nowValue)
	if err != nil {
		return err
	}
	query := clients.TrendingQuery{CreatedAfter: createdAfter, MinStars: minStars, Language: language}
	key := trendingcache.Key(since, minStars, language)
	cachePath := trendingcache.Path(rt.dataPath)

	cache := trendingcache.Load(cachePath)
	if repos, ok := cache.Fresh(key, nowValue, trendingcache.DefaultTTL); ok {
		return writeTrendingOutput(out, repos, jsonOutput)
	}

	repos, err := rt.github.SearchTrending(context.Background(), query)
	if errors.Is(err, clients.ErrTrendingRateLimited) {
		if repos, ok := cache.Any(key); ok {
			fmt.Fprintln(errOut, "警告: GitHub Search API のレート制限に達しました。キャッシュ済みの一覧を表示します。時間をおいて再実行してください")
			return writeTrendingOutput(out, repos, jsonOutput)
		}
		return errors.New("GitHub Search API のレート制限に達しました。時間をおいて再実行してください")
	}
	if err != nil {
		return errors.New("GitHub のトレンド一覧を取得できませんでした")
	}
	cache.Set(key, repos, nowValue)
	if err := trendingcache.Save(cachePath, cache); err != nil {
		fmt.Fprintln(errOut, "警告: トレンド一覧のキャッシュを保存できませんでした")
	}
	return writeTrendingOutput(out, repos, jsonOutput)
}
