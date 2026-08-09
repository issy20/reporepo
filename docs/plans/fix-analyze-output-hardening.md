# Plan: analyzeコマンドの出力サニタイズと警告出力先の修正

Status: draft

## 目的

コードレビュー指摘2件を解消する。

1. `reporepo analyze` の plain 出力で `Keywords` が `safeText` を通っていないため、AI出力に制御文字・ANSIを仕込むとターミナルへ注入され得る（`cmd/analyze_output.go`）。Summary / TechStack / Background は `safeText` 済みで Keywords だけが漏れている。
2. `runAnalyze` が警告を `deps.app.warn`（`os.Stderr` 直書き）で出力し、Cobra の writer（`cmd.ErrOrStderr()`）を尊重しない。SPEC 2.8 の「テストや埋め込み利用でCobraのwriterを差し替えた場合はそのwriterを尊重する」に反する。

## 前提

- analyze コマンドの出力は常に plain text（decorated表示は行わない）。
- TUI の `detailMarkdown` は Keywords を `safeText` 済みのため、修正は CLI 経路のみ。
- JSON 出力は `json.Encoder` が制御文字をエスケープするため、サニタイズ不要。
- `buildRuntime` の呼び出し元は `runApplicationWith` と `runAnalyze` の2箇所のみ。run（TUI）経路の警告挙動は変えない。

## スコープ

### 対象

- `cmd/analyze_output.go`: `formatAnalyzePlain` の Keywords サニタイズ
- `cmd/analyze.go`: `runAnalyze` に `errOut` を追加し、警告を `cmd.ErrOrStderr()` 経由で出力
- `cmd/application.go`: `buildRuntime` に警告出力コールバックを追加
- `cmd/analyze_test.go`: 警告出力先テストの更新＋Keywordsサニタイズテスト追加

### 対象外

- `reporepo run`（TUI）経路の警告出力（従来どおり `os.Stderr`）
- エラー出力の既存ルート（`executeRoot` の `renderer.Error`）
- TUI 表示、JSON 出力の形式変更

## 設計

### 変更1: Keywords のサニタイズ（analyze_output.go）

`formatAnalyzePlain` の Keywords 出力を TUI の `detailMarkdown` と同様に `safeText` 経由にする。

```go
b.WriteString("\n# Keywords\n")
keywords := make([]string, len(a.Keywords))
for i, keyword := range a.Keywords {
	keywords[i] = safeText(keyword)
}
b.WriteString(strings.Join(keywords, ", "))
```

### 変更2: 警告の出力先を Cobra writer へ（analyze.go / application.go）

`buildRuntime` は警告出力コールバックを受け取る形へ変更する。`deps.warn`（`os.Stderr` 直書き）は run 経路のまま維持する。

```go
func buildRuntime(deps applicationDependencies, warn func(string)) (*runtime, error)
```

- `runApplicationWith`: `buildRuntime(deps, deps.warn)`（挙動不変）
- `runAnalyze`: `buildRuntime(*deps.app, func(msg string) { fmt.Fprintln(errOut, "警告:", msg) })`

`runAnalyze` に `errOut io.Writer` を追加し、結果警告も同じ形式（`警告: <message>`）で errOut へ出力する。

```go
func runAnalyze(deps commandDependencies, input, provider, language string, jsonOutput, force bool, out, errOut io.Writer) error
```

`newAnalyzeCommand` の `RunE` で `cmd.ErrOrStderr()` を渡す。警告プレフィックス `警告:` は既定の `warn` と同じ形式を維持する。

## テストリスト

### A. Keywords のサニタイズ（analyze_output.go）

- [ ] Keywords に ANSI エスケープが含まれる解析で、plain 出力に `\x1b[` が含まれない
- [ ] Keywords に制御文字（`\x00` 等）が含まれる場合に除去され、正常なキーワードは維持される
- [ ] Summary / TechStack / Background の `safeText` 挙動が変わらない（回帰）
- [ ] 既存の plain 出力テストが全て通る（回帰）

### B. 警告の出力先（analyze.go / application.go）

- [ ] キャッシュヒット時の警告が stdout でなく errOut（`ErrOrStderr`）へ出力される
- [ ] 警告に `警告:` プレフィックスが付与される
- [ ] secret 解決時の警告も errOut へ出力される
- [ ] run（TUI）経路の警告は従来どおり `os.Stderr` へ出力される（回帰）
- [ ] 既存テスト（`runApplicationWith` / analyze）が全て通る（回帰）

## 実装順序

### Step 1: テスト（red）

- `analyze_test.go`: Keywords に ANSI・制御文字を含む解析で plain 出力を検証するテストを追加して失敗を確認する。
- `analyze_test.go`: `TestAnalyzeWarningsGoToStderr` を、警告が errOut へ出力されることを検証する形へ更新して失敗を確認する。

### Step 2: 実装（green）

- `analyze_output.go`: Keywords を `safeText` 経由にする。
- `analyze.go`: `runAnalyze` に `errOut` を追加し、`newAnalyzeCommand` から `cmd.ErrOrStderr()` を渡す。結果警告を errOut へ出力する。
- `application.go`: `buildRuntime` に warn コールバックを追加し、`runApplicationWith` は `deps.warn` を渡す。

### Step 3: 検証

```bash
gofmt -l .            # 空であること
go test ./...
go test -race ./...
go vet ./...
```

## 完了条件

- `reporepo analyze` の plain 出力に制御文字・ANSI が含まれない。
- analyze の警告（結果警告・secret解決警告）が `cmd.ErrOrStderr()` へ出力され、stdout へ漏れない。
- `reporepo run`（TUI）経路の警告出力が不変。
- `gofmt` / `go test ./...` / `go test -race ./...` / `go vet ./...` が全て成功する。

## 想定される変更

- `cmd/analyze_output.go`: Keywords の `safeText` 適用
- `cmd/analyze.go`: `runAnalyze` の `errOut` 追加、警告出力の置き換え、`cmd.ErrOrStderr()` の受け渡し
- `cmd/application.go`: `buildRuntime` の warn コールバック追加
- `cmd/analyze_test.go`: 警告出力先テストの更新＋Keywordsサニタイズテスト追加

## worktree

- branch: `fix-analyze-output-hardening`
- worktree path: `/Users/issy20/ccplayground/reporepo/fix-analyze-output-hardening`

理由: レビュー指摘2件の修正であり、対象が `cmd` パッケージに収まる小規模変更のため、同一worktreeで進める。
