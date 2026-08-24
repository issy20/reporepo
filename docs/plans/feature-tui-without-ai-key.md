# Plan: AI API key なしでの TUI 起動を可能にする

Status: draft

## 目的

現在、TUI（`run` / ルートコマンド）は起動時に AI API key が必須であり、未設定だと `ANTHROPIC_API_KEY、OPENAI_API_KEY、GEMINI_API_KEY のいずれかを設定してください` で終了する。しかし疑似Trending一覧（SPEC 2.12）の実装により、TUI 上で AI key なしに動作する機能（Trending閲覧・履歴・お気に入り・ノート）が増えた。

本計画では TUI 起動時の AI key 必須要件を外し、AI key なしでも TUI を起動して AI を必要としない機能を利用できるようにする。AI key が必要になるのは `analyze`（解析実行）時だけとし、その際は設定方法を案内する。

`reporepo analyze` コマンド（SPEC 2.11）は解析を主目的とするため、従来どおり AI key 必須を維持する。

## 前提

- `buildRuntime`（`cmd/application.go:88`）は `requireAI bool` フラグを持つ。`true` のとき `resolveRuntimeSecrets`（`cmd/secrets.go:23`）が AI key 未設定エラーと provider フォールバックを行う。
- `runApplicationWith`（`cmd/application.go:187`）は `buildRuntime(deps, deps.warn, true)` を呼び、**TUI 起動時に AI key を必須**にしている。ここが変更点。
- `runAnalyze`（`cmd/analyze.go:38`）は `buildRuntime(..., true)` で AI 必須。`runAnalyze` の provider チェック（`cmd/analyze.go:51-52`）も AI 未設定エラーを出す。
- `runTrending`（`cmd/trending.go:53`）は `buildRuntime(..., false)` で AI key なしでも動作済み。
- TUI 本体は空の AI マップを処理できる。`availableProviders`（`internal/tui/model.go:132`）は空マップで空スライスを返し、`NewModel`（`model.go:102-105`）と `nextProvider`（`update.go:161`）は空プロバイダを安全に扱う。
- 解析失敗時、`analysisFailedMsg` ハンドラ（`internal/tui/update.go:44-57`）が `m.errMessage` にエラーを設定し、`viewInput`（`internal/tui/view.go:97-100`）が表示する。ただし現状の文言は `AI provider "claude" は利用できません`（`internal/analyzer/analyzer.go:74`）で、設定方法の案内が無い。

## スコープ

### 対象

- `cmd/application.go`: `runApplicationWith` の `buildRuntime` 呼び出しを `requireAI=false` に変更
- `cmd/application_test.go`: AI key なしで TUI が起動するテスト追加、既存の必須化テストの改訂
- `internal/tui/update.go`: `startAnalysis`（`update.go:252`）で AI provider が無いときの案内メッセージ表示
- `internal/tui/update_test.go`: 案内メッセージのテスト追加
- `SPEC.md`: 2.7 / 2.11 の AI key 必須に関する記述を新挙動に整合

### 対象外

- `reporepo analyze` コマンドの AI 必須要件（`cmd/analyze.go` / `runAnalyze` は変更しない）
- `cmd/secrets.go` の `resolveRuntimeSecrets` の `requireAI` 分岐ロジック自体（挙動は維持）
- `internal/analyzer` の provider 未設定エラー文言の変更
- TUI で AI key を設定する新UI（設定は `reporepo config` に委ねる）

## 設計

### 変更1: TUI 起動の `requireAI` を false に（cmd/application.go）

`runApplicationWith` の `buildRuntime` 呼び出しを変更する。

```go
rt, err := buildRuntime(deps, deps.warn, false) // 変更前: true
```

- `requireAI=false` のとき `resolveRuntimeSecrets`（`cmd/secrets.go:32-34`）は AI key 未設定エラーと provider フォールバックをスキップする。GitHub token の解決は従来どおり行われる。
- TUI は空の AI マップで起動し、Trending・履歴・お気に入り・ノートを利用できる。
- `runtimeConfig.DefaultProvider` のフォールバックは効かなくなるが、TUI 側で `availableProviders`（`model.go:102-105`）が利用可能 provider から選ぶため問題ない。AI が空のときは `NewModel` の既定 `"claude"`（`model.go:93`）のまま解析時に案内が出る。

### 変更2: analyze 実行時の AI key 未設定案内（internal/tui/update.go）

`startAnalysis` の冒頭で、利用可能な AI provider が無ければ解析を開始せずに案内メッセージを表示して留まる。

```go
func (m Model) startAnalysis(input string, force bool) (tea.Model, tea.Cmd) {
	if m.mutationPending {
		return m, nil
	}
	if len(availableProviders(m.ai)) == 0 {
		m.errMessage = "AIのAPI keyが設定されていません。`reporepo config` で設定できます"
		return m, nil
	}
	// 既存ロジック（stateLoading へ遷移、analyzeCmd を返す）
}
```

- `availableProviders`（`model.go:132`）は既存の公開関数で、空 AI マップで空スライスを返す。
- 入力画面の `enter`（`update.go:135`）と Trending 一覧の `enter`（`update.go:294`）の両経路が `startAnalysis` を通るため、どちらでも案内が表示される。
- `m.errMessage` は `viewInput`（`view.go:97-100`）が `errorStyle` + `safeText` で表示済み。バッククォートは `safeText` で維持される（制御文字のみ除去）。
- 状態は `stateInput` のままなので、`viewInput` の `errMessage` 表示領域（`view.go:61-63, 97-100`）で表示される。

### SPEC の整合

- **2.7**（`SPEC.md:71`）: 「AI provider の secret が取得できない場合は安全なエラーと設定方法を表示する」を「TUI は AI key なしで起動でき、解析実行時に設定方法を案内する。`analyze` コマンドは従来どおり AI key 必須」に更新する。
- **2.11**（`SPEC.md:186`）: 「run と同一の経路」の記述は `run` 側が AI key 必須でなくなったため、`analyze` は明示的に AI key 必須であることを記す。

## テストリスト

### A. TUI 起動が AI key なしで成功（cmd/application_test.go）

- [ ] AI key を一切設定せず `runApplicationWith` がエラーを返さず、`runTUI` が呼ばれ `deps.AI` が空マップである
- [ ] AI key なしでも GitHub token の解決と GitHub クライアント構築が行われる（Trending が使える構成）
- [ ] 既存 `TestRunApplicationRejectsMissingAIKeysBeforeResolvingDataPath`（`application_test.go:597`）を「AI key なしでも起動成功しデータパスが解決される」に改訂
- [ ] `reporepo analyze`（`runAnalyze`）は AI key なしのとき従来どおりエラーを返す（回帰、変更なしを確認）

### B. analyze 実行時の AI key 未設定案内（internal/tui/update_test.go）

- [ ] `m.ai` が空の Model で `startAnalysis` を呼ぶと `stateInput` のまま、`m.errMessage` に「設定されていません」と設定方法の案内が入り、解析（analyzeCmd）が開始されない
- [ ] `m.ai` に provider がある場合は従来どおり `stateLoading` へ遷移し解析が開始される（回帰）
- [ ] 入力画面 `enter` と Trending 一覧 `enter` の両経路で AI key なしの場合に案内が表示される

### C. 表示（internal/tui/view_test.go）

- [ ] AI key 未設定の `errMessage` が `viewInput` にエラー表示される（既存表示経路の回帰）

### D. 回帰

- [ ] 既存の `cmd` / `internal/tui` テストが全て通る
- [ ] `gofmt -l .` / `go test ./...` / `go test -race ./...` / `go vet ./...` が成功

## 実装順序

### Step 1: テスト（red）

- `cmd/application_test.go`: AI key なしで `runApplicationWith` が成功し `deps.AI` が空であるテストを書き、現状失敗（エラーで終了）を確認
- `internal/tui/update_test.go`: `startAnalysis` が AI key なしで案内メッセージを設定し解析を開始しないテストを書き、現状失敗（`stateLoading` に遷移）を確認

### Step 2: 実装（green）

- `cmd/application.go`: `runApplicationWith` の `buildRuntime` を `requireAI=false` に変更
- `internal/tui/update.go`: `startAnalysis` に AI provider 空チェックを追加
- `cmd/application_test.go`: 既存の必須化テスト（`TestRunApplicationRejectsMissingAIKeysBeforeResolvingDataPath`）を新挙動に改訂

### Step 3: リファクタ

- 変更に伴い不要になる分岐や重複がないか確認（`requireAI` フラグは `analyze` で引き続き使用するため維持）

### Step 4: SPEC 更新

- `SPEC.md` の 2.7 / 2.11 記述を新挙動に整合

### Step 5: 検証

```bash
gofmt -l .            # 空であること
go test ./...
go test -race ./...
go vet ./...
```

## 完了条件

- AI key を設定していなくても TUI が起動し、Trending 閲覧・履歴・お気に入り・ノートを利用できる
- AI を必要とする解析を TUI 内で実行しようとしたとき、`reporepo config` での設定方法を案内するメッセージが表示される
- `reporepo analyze` コマンドは従来どおり AI key 未設定でエラーを返す
- `SPEC.md` の該当記述が新挙動と整合する
- `gofmt` / `go test ./...` / `go test -race ./...` / `go vet ./...` が全て成功する

## 想定される変更

- `cmd/application.go`: `runApplicationWith` の `buildRuntime` 呼び出し（`true` → `false`）
- `cmd/application_test.go`: AI key なし起動テスト追加、既存必須化テスト改訂
- `internal/tui/update.go`: `startAnalysis` への AI provider 空チェック追加
- `internal/tui/update_test.go` / `view_test.go`: 案内メッセージのテスト追加
- `SPEC.md`: 2.7 / 2.11 の記述更新

## worktree

- branch: `feature-tui-without-ai-key`
- worktree path: `/Users/issy20/ccplayground/reporepo/feature-tui-without-ai-key`

理由: TUI 起動要件の変更という独立した機能で、他の作業と混在させずレビュー・リバートを明確にするため、専用 worktree で進める。
