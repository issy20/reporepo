# Plan: 疑似Trending一覧（学びのネタ探し）

Status: draft

## 目的

SPEC 2.12 の実装。製品主題である「リポジトリ単位の学習」の入口として、GitHub Search API で「直近に作成され、スターが伸びた repo」の一覧を近似して提供する。

- Phase 1: CLI `reporepo trending` コマンド（Search API クライアント + 一覧キャッシュ + plain/JSON 出力）
- Phase 2: TUI 入力画面の `t` キーによる Trending 一覧導線

## 前提

- GitHub に公式の Trending API は存在しないため、Search API で近似する（2.12）。
- Trending は学習の入口であり、**AI キー未設定のユーザーでも動くべき**。既存の `buildRuntime` / `resolveRuntimeSecrets` は AI キー必須のため、`requireAI` 指定を追加する。
- 検索結果の repo 名・説明文は攻撃者制御データ。表示には既存の `safeText`（`cmd/analyze_output.go`）を利用する。
- Search API はレート制限（トークン付き30回/分、未認証10回/分）が厳しいため、一覧は数時間キャッシュする。
- `GitHubClient` インターフェースは TUI・analyzer・CLI の境界。`SearchTrending` はここへ追加し、テスト fake（`stubGitHubClient` / `fakeGitHub`）へ空実装を足す。

## スコープ

### 対象（Phase 1）

- `internal/clients/github.go`: `TrendingQuery` / `TrendingRepo` / `ErrTrendingRateLimited` / `Client.SearchTrending`
- `cmd/trending.go`: `reporepo trending` コマンドと `runTrending`
- `cmd/trending_cache.go`: TTL 付きファイルキャッシュ
- `cmd/trending_output.go`: plain / JSON 出力
- `cmd/application.go`: `buildRuntime` に `requireAI` 追加、`runtime.dataPath` 追加
- `cmd/secrets.go`: `resolveRuntimeSecrets` に `requireAI` 追加
- テスト: `github_test.go` / `trending_test.go` / `secrets_test.go` / `application_test.go` / `analyze_test.go`

### 対象（Phase 2）

- `internal/tui/`: 入力画面の `t` キー導線、Trending 一覧の非同期取得と選択

### 対象外

- 正確なトレンド指標（velocity）の算出
- `--refresh` 等の明示更新フラグ（TTL のみで十分）
- 検索結果のページング・100 件超
- 一覧からのバッチ解析
- Phase 2 の詳細設計（Phase 1 完了後に固める）

## 設計

### 変更1: Search API クライアント（github.go）

`Since` 文字列→日付の変換は呼び出し側（cmd）で行い、決定性・テスト容易性のため `CreatedAfter time.Time` を渡す。

```go
type TrendingQuery struct {
	CreatedAfter time.Time
	MinStars     int
	Language     string
	Limit        int
}

type TrendingRepo struct {
	FullName    string
	Description string
	Stars       int
	Language    string
}

var ErrTrendingRateLimited = errors.New("trending rate limit exceeded")

func (c *Client) SearchTrending(ctx context.Context, q TrendingQuery) ([]TrendingRepo, error)
```

- クエリ: `created:>YYYY-MM-DD stars:>N fork:false archived:false`（`Language` 指定時は `language:X` を追加）
- `sort=stars&order=desc&per_page=Limit`（既定 `30`）
- 403（`X-RateLimit-Remaining: 0`）または 429 → `ErrTrendingRateLimited`
- 応答 body は `io.LimitReader`（例: 1 MiB）で読み取り上限
- `GitHubClient` インターフェースへ `SearchTrending` を追加し、fake を追随させる

### 変更2: AI キー非必須化（application.go / secrets.go）

`resolveRuntimeSecrets` と `buildRuntime` に `requireAI bool` を追加する。

- `resolveRuntimeSecrets(cfg, store, requireAI)`:
  - `requireAI=false` のとき、AI キー未設定エラーと provider フォールバックをスキップする（secret 解決と GitHub token のみ行う）
- `buildRuntime(deps, warn, requireAI)`:
  - `requireAI=false` のとき AI クライアントを構築しない
  - `runtime` に `dataPath string` を追加し、キャッシュ保存先を導出可能にする
- `runApplicationWith` / `runAnalyze` は `requireAI=true`（挙動不変）
- 新規 `runTrending` は `requireAI=false`

### 変更3: trending コマンドとキャッシュ（cmd/trending.go / trending_cache.go / trending_output.go）

```
reporepo trending [--since today|week|month] [--language X] [--min-stars N] [--json]
```

| フラグ | 既定 | 意味 |
|---|---|---|
| `--since` | `week` | today=1日 / week=7日 / month=30日 |
| `--language` | なし | 言語絞り込み |
| `--min-stars` | `50` | 品質下限 |
| `--json` | false | JSON 配列出力 |

`runTrending(deps, since, language string, minStars int, jsonOutput bool, now func() time.Time, out, errOut io.Writer) error`:

1. `buildRuntime(*deps.app, warn, false)` でランタイム構築（AI キー不要）
2. `since` → `CreatedAfter` 変換（`now` はテストで注入）
3. キャッシュ確認: `<dataPath ディレクトリ>/trending-cache.json` の該当クエリが `DefaultTrendingCacheTTL = 6h` 以内なら使用
4. ミスなら `SearchTrending` → キャッシュ保存
5. レート制限エラー時: キャッシュがあれば（古くても）表示し、警告（stderr）で案内。キャッシュがなければ安全なエラー
6. 出力: plain（1 行 1 repo、`safeText` 適用）or JSON

キャッシュ構造（クエリ別エントリ、キーは `since|minStars|language` の正規化文字列）:

```json
{
  "version": 1,
  "entries": {
    "week|50|go": { "fetched_at": "...", "repos": [ { "full_name": "...", "description": "...", "stars": 123, "language": "Go" } ] }
  }
}
```

- 壊れた JSON はミス扱い
- キャッシュの読み書きはアトミック（一時ファイル + rename、0600）

### 変更4: TUI（Phase 2）

入力画面の `t` キーで Trending 一覧を表示する導線を追加する。非同期コマンドで `SearchTrending` を取得し、一覧 → 選択 → 既存の `analyzer.Analyze`（キャッシュ・鮮度・保存）へ流し込む。詳細設計は Phase 1 完了後に別計画で固める。

## テストリスト

### A. Search API クライアント（github_test.go）

- [ ] `CreatedAfter` / `MinStars` / `Language` から正しいクエリを組み立てる（言語あり・なし）
- [ ] 検索応答を `[]TrendingRepo` へパースする
- [ ] 403（rate limit）→ `ErrTrendingRateLimited`
- [ ] 429 → `ErrTrendingRateLimited`
- [ ] その他エラー（非 2xx）を返す
- [ ] 応答 body の読み取りが上限で制限される
- [ ] 結果ゼロは空スライスを返す

### B. キャッシュ（trending_cache_test.go）

- [ ] 新鮮なキャッシュがあれば再取得しない
- [ ] 期限切れなら再取得する
- [ ] クエリが異なれば別エントリとして扱う
- [ ] 壊れたキャッシュはミス扱い
- [ ] レート制限時に古いキャッシュがあればそれを表示する
- [ ] レート制限時にキャッシュがなければ安全なエラー

### C. コマンド・出力（trending_test.go）

- [ ] `reporepo trending` が 1 行 1 repo で出力する
- [ ] 説明文の制御文字・ANSI が除去される
- [ ] `--json` が正しい JSON 配列を出力する
- [ ] `--since` / `--language` / `--min-stars` がクエリへ反映される
- [ ] AI キー未設定でも動作する（`requireAI=false`）
- [ ] 一覧は stdout、警告・エラーは stderr、終了コード 0/1
- [ ] secret が出力に含まれない

### D. 回帰

- [ ] run / analyze は AI キー必須のまま（`requireAI=true`）
- [ ] `resolveRuntimeSecrets` / `buildRuntime` 既存テストが通る（回帰）
- [ ] `GitHubClient` fake（`stubGitHubClient` / `fakeGitHub`）がコンパイルを通る（回帰）
- [ ] 既存テスト全体が通る（回帰）

### E. TUI（Phase 2）

- [ ] `t` キーで Trending 一覧を表示する
- [ ] 選択した repo を既存パイプラインで解析・保存する
- [ ] 既存 TUI テストが通る（回帰）

## 実装順序

### Step 1: テスト（red）

- `github_test.go`: SearchTrending のクエリ構築・パース・レート制限テスト
- `secrets_test.go`: `resolveRuntimeSecrets` の `requireAI` テスト（false で AI キー未設定でも成功）

### Step 2: 実装（green）

- `github.go`: `SearchTrending` と型・`ErrTrendingRateLimited` を追加し、`GitHubClient` インターフェースと fake を更新
- `secrets.go` / `application.go`: `requireAI` 追加、`runtime.dataPath` 追加

### Step 3: キャッシュとコマンド（red → green）

- キャッシュテスト → `trending_cache.go` 実装
- コマンド・出力テスト → `trending.go` / `trending_output.go` 実装、`root.go` へ登録

### Step 4: 検証

```bash
gofmt -l .            # 空であること
go test ./...
go test -race ./...
go vet ./...
```

### Step 5: TUI（Phase 2）

Phase 1 のマージ後に別計画として着手する。

## 完了条件

- `reporepo trending` が Search API で一覧を取得し、plain / JSON で出力する
- 一覧は 6 時間キャッシュされ、レート制限時はキャッシュへフォールバックする
- AI キー未設定でも動作する
- 出力に制御文字・ANSI・secret が含まれない
- `gofmt` / `go test ./...` / `go test -race ./...` / `go vet ./...` が全て成功する

## 想定される変更

- `internal/clients/github.go`: `TrendingQuery` / `TrendingRepo` / `ErrTrendingRateLimited` / `SearchTrending` 追加、`GitHubClient` インターフェース拡張
- `internal/clients/github_test.go`: SearchTrending テスト追加
- `cmd/trending.go` / `trending_cache.go` / `trending_output.go`: 新規
- `cmd/trending_test.go` / `trending_cache_test.go`: 新規
- `cmd/application.go`: `buildRuntime` の `requireAI` 追加、`runtime.dataPath` 追加
- `cmd/secrets.go`: `resolveRuntimeSecrets` の `requireAI` 追加
- `cmd/root.go`: `trending` コマンド登録
- `cmd/application_test.go` / `cmd/analyze_test.go`: fake の `SearchTrending` 追加

## worktree

- branch: `feature-trending`
- worktree path: `/Users/issy20/ccplayground/reporepo/feature-trending`

理由: SPEC 2.12 の新機能であり、`internal/clients`・`cmd` の追加と `application.go` / `secrets.go` の小改修に収まる。Phase 2（TUI）はこのworktreeでも、マージ後の別worktreeでもよいが、Phase 1 の完了を先に確認する。
