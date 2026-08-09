# Plan: analyze コマンド（非対話・自動化）

Status: draft

## 目的

TUI を起動せずにリポジトリを解析し、結果を stdout へ出力する `reporepo analyze owner/repo` を追加する。スクリプト・CI・パイプからの利用を可能にする（SPEC 2.11）。設定・secret・クライアント構築は `run` と同一経路を使い、解析は共有パイプライン `internal/analyzer` を利用する。

## 前提と技術選定

- `internal/analyzer` が `feature-code-analysis` で抽出済み、鮮度管理が `feature-cache-freshness` で適用済みであること。
- 出力は常に ANSI を含まない plain text（TTY でも装飾しない）。解析結果そのものがデータであり、パイプ・ページャーでの利用を前提とするため、CLI プレゼンテーションの装飾対象から意図的に除外する（SPEC 2.11）。
- エラー・警告は `presentation.Renderer` 経由で stderr へ出力する。
- 引数はリポジトリ 1 つ（`cobra.ExactArgs(1)`）。複数引数・stdin 一括は将来拡張（SPEC 2.11）。
- `run` と `analyze` で設定・client 構築を共有するため、`cmd/application.go` からランタイム構築を抽出する。

## スコープ

### 対象

- `cmd/application.go` からのランタイム構築抽出（`run` と `analyze` の共通化）
- Cobra `analyze` コマンド（引数・フラグ）
- plain / JSON の出力フォーマット
- キャッシュ・鮮度・入力バージョン連携（`Analyzer` 経由）
- 警告・エラーの stderr 分離と終了コード
- SPEC 2.11 に沿ったテスト・README の更新

### 対象外

- 複数引数・stdin からの一括解析（シェルループで構成。将来拡張）
- streaming 表示、spinner
- `--no-save` 等の追加フラグ
- 出力の Markdown レンダリング（plain text のまま）
- 並列解析・リトライ

## 設計

### ランタイム構築の共有化（refactor）

`cmd/application.go` の `runApplicationWith` から、次の共通処理を `buildRuntime` へ抽出する。

```go
type runtime struct {
    cfg    *core.Config
    github clients.GitHubClient
    ai     map[string]clients.AIClient
    store  *store.Store
}

func buildRuntime(deps applicationDependencies) (*runtime, error)
```

- 設定読み込み（legacy 移行含む）→ secret 解決 → データパス解決 → GitHub / AI client 構築。
- 警告は `deps.warn` へ、エラーは既存のユーザー向けメッセージへ変換する。
- `runApplicationWith` は `buildRuntime` を呼び、そのまま TUI を起動する（振る舞い不変）。
- `runApplicationWith` の既存テスト（`cmd/application_test.go`）で回帰を確認する。

### コマンド定義

`cmd/analyze.go` を新設する。

```go
func newAnalyzeCommand(deps commandDependencies) *cobra.Command
```

- `Use: "analyze OWNER/REPO"`、`Args: cobra.ExactArgs(1)`、`SilenceUsage: true`。
- `root.go` の `AddCommand` へ追加する。
- フラグ:

| フラグ | 既定 | 意味 |
|---|---|---|
| `--provider, -p` | `config.json` の `default_provider` | AI プロバイダ |
| `--language, -l` | `config.json` の `default_language` | 出力言語 ja/en |
| `--json` | false | JSON 出力 |
| `--force, -f` | false | キャッシュを無視して再生成 |

- `RunE` の流れ:
  1. `buildRuntime(deps)` で client 群を構築。
  2. 指定 provider が `ai` になければ「API key が設定されていません。`reporepo config` で設定できます」を返す。
  3. `analyzer.New(rt.store, rt.github, rt.ai, time.Now, refreshInterval)` を組み立てる。
  4. `Analyze(ctx, input, language, provider, force)` を実行。
  5. `Result.Warnings` を stderr へ、結果を stdout へ出力。

### 出力フォーマット

**plain（既定）:** メタ情報ヘッダと4セクションを `fmt.Fprintln(cmd.OutOrStdout(), ...)` で出力する。装飾・ANSI を含めない。

```
owner/repo
⭐ 12345  Forks 123  Language Go
取得: 3日前  解析: 5日前

# Summary
...
# Tech Stack
...
# Background
...
# Keywords
a, b, c
```

- ヘッダの取得・解析日時は `RepoMeta.FetchedAt` と `Analysis.CreatedAt` から相対表示（鮮度管理の表示ロジックと揃える）。
- 解析が古い（`IsStale`）場合は末尾に「解析はリポジトリ更新前のものです（--force で再生成）」を 1 行追加する。

**`--json`:** 単一 JSON オブジェクトを `encoding/json` で出力する。

```go
type analyzeJSONOutput struct {
    FullName  string          `json:"full_name"`
    Repo      *core.RepoMeta  `json:"repo"`
    Analysis  *analysisOutput `json:"analysis"`
    Language  string          `json:"language"`
    Provider  string          `json:"provider"`
    Model     string          `json:"model"`
    CreatedAt time.Time       `json:"created_at"`
}
type analysisOutput struct {
    Summary    string   `json:"summary"`
    TechStack  string   `json:"tech_stack"`
    Background string   `json:"background"`
    Keywords   []string `json:"keywords"`
}
```

- secret は含めない。`CreatedAt` は表示中の解析の生成日時。

出力整形関数は `cmd/analyze.go`（または `cmd/analyze_output.go`）に置き、`internal/tui` へ依存させない。`internal/tui` の `detailMarkdown` とは独立した plain text 整形として実装する。

### エラー・警告・終了コード

- 警告: `Result.Warnings` を `deps.warn` 経由で stderr へ出力する（`presentation.Renderer` の装飾は尊重してよい）。
- エラー: `RunE` がユーザー向けエラーを返し、`executeRoot` が `renderer.Error` で stderr へ描画する（既存フロー）。
- 終了コード: 成功 0 / 失敗 1（`main` の既存 `os.Exit(run(...))`）。
- 不正な repo 形式・解析失敗は `Analyzer` 由来の secret 非包含エラーをそのまま返す。

## TDDテストリスト

実装時は一度に一件だけ選び、red → green → refactor を繰り返す。

### A. ランタイム共有化（回帰）

- [ ] `buildRuntime` が run と同じ設定・secret・client を構築する
- [ ] `runApplicationWith` の既存テストがすべて通る（回帰）
- [ ] 設定読み込み失敗・secret 未設定のエラー変換が維持される

### B. コマンド配線

- [ ] `reporepo analyze`（引数なし）がエラーを返す
- [ ] `reporepo analyze owner/repo` が解析して結果を出力する
- [ ] `--help` に analyze が表示される
- [ ] 無効な引数（2つ以上）がエラーを返す

### C. フラグ

- [ ] `--provider` / `-p` が既定 provider より優先される
- [ ] `--language` / `-l` が既定言語より優先される
- [ ] 未設定 provider を指定すると設定案内エラーを返す
- [ ] `--force` / `-f` で再生成する
- [ ] `--json` で JSON 出力になる

### D. 出力フォーマット

- [ ] plain 出力にメタヘッダと 4 セクションを含む
- [ ] plain 出力が ANSI を含まない（装飾済みでも）
- [ ] `--json` が有効な JSON（`encoding/json` でパースできる）
- [ ] JSON に `full_name` / `repo` / `analysis` / `language` / `provider` / `model` / `created_at` を含む
- [ ] 出力に secret を含まない
- [ ] stale な解析で plain・JSON 双方に再生成案内を反映する

### E. パイプライン連携

- [ ] キャッシュヒットで AI を呼ばず保存済みを出力する
- [ ] `--force` で再生成して保存する
- [ ] 鮮度リフレッシュ（メタ再取得）が適用される
- [ ] 解析結果が `data.json` に保存される（TUI の履歴に現れる）
- [ ] Warnings が stderr へ出力される

### F. エラー処理

- [ ] 不正な repo 形式でエラー（secret 非包含）
- [ ] 解析失敗でエラー（secret 非包含）
- [ ] stdout / stderr が分離される
- [ ] 成功時に終了コード 0、失敗時に 1 を返す（`main` の `run` で検証）

## 実装順序

### Step 1: ランタイム共有化（refactor）

1. `buildRuntime` を抽出し、`runApplicationWith` を書き換える。
2. `cmd/application_test.go` で回帰を確認する。

### Step 2: コマンド配線

1. `newAnalyzeCommand` を追加し、`root.go` に登録する。
2. 引数1つ・エラー時のテスト（B）を red から green へ進める。

### Step 3: フラグ処理

1. `--provider` / `--language` / `--json` / `--force` を実装する。
2. 未設定 provider のエラーを実装する。

### Step 4: 出力フォーマット

1. plain 出力と JSON 出力を実装する。
2. 日時表示・stale 案内を実装し、テスト（D）を通す。

### Step 5: パイプライン連携とエラー処理

1. `Analyzer` のキャッシュ・鮮度・警告を出力へ接続する。
2. stdout / stderr 分離と終了コードを検証する。

### Step 6: 統合と文書

1. 全テスト・race・vet・build・`make smoke` を実行する。
2. SPEC 2.11・9.3 の実装状況を更新し、README のコマンド表へ analyze を追記する。

## 完了条件

- `reporepo analyze owner/repo` で TUI なしに解析でき、plain / `--json` で出力できる。
- フラグ（provider / language / json / force）が仕様どおり動作する。
- キャッシュ・鮮度・入力バージョンが `run`（TUI）と同一の挙動になる。
- 出力は常に ANSI なし。エラー・警告は stderr、結果は stdout、終了コードは 0/1。
- secret が stdout・stderr・JSON のいずれにも含まれない。
- 既存 `run` / `config` / `version` / `where` が回帰しない。
- 次のコマンドがすべて成功する。

```bash
gofmt -l .
go test ./...
go test -race ./...
go vet ./...
CGO_ENABLED=0 go build ./...
make smoke
```

## 想定される変更

- `cmd/analyze.go`: analyze コマンド定義・フラグ・RunE（新規）
- `cmd/analyze_test.go`: 配線・フラグ・出力・エラー処理のテスト（新規）
- `cmd/analyze_output.go`: plain / JSON の整形（新規）
- `cmd/analyze_output_test.go`: 出力フォーマットのテスト（新規）
- `cmd/application.go`: `buildRuntime` 抽出
- `cmd/application_test.go`: 回帰追随
- `cmd/root.go`: `newAnalyzeCommand` 登録
- `SPEC.md` / `README.md`: 2.11 の実装状況とコマンド表

## worktree

- branch: `feature-analyze-command`
- worktree path: `/Users/issy20/ccplayground/reporepo/feature-analyze-command`

理由: `feature-code-analysis` の `internal/analyzer` と `feature-cache-freshness` の鮮度管理を利用するため、両ブランチの後に実装する。`buildRuntime` 抽出は `cmd` パッケージ内の refactor であり、`run` の回帰テストで安全性を確認する。
