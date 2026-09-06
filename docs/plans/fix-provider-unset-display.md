# Plan: TUI で AI provider 未設定時に「未設定」と表示する

Status: draft

## 目的

`NewModel`（`internal/tui/model.go:93`）は provider の初期値を `"claude"` に固定している。AI API key 未設定（`deps.AI` が空）かつ `config.json` に `default_provider` が無い場合、利用可能 provider が無いにもかかわらず `provider` が `"claude"` のままとなり、入力画面に `provider: claude` と表示される。実際にはどの AI provider も設定されていないのに claude が選択されているように見える誤表示を解消し、未設定なら「未設定」と表示する。

## 前提

- `NewModel`（`model.go:92-130`）: provider 初期値 `"claude"`（`model.go:93`）。cfg の `DefaultProvider` が有効値なら上書き（`model.go:98-100`）。利用可能 provider が1つ以上あり、`provider` がその中に無ければ最初の利用可能 provider へ補正（`model.go:102-105`）。
- `availableProviders`（`model.go:132-140`）: `deps.AI` から利用可能 provider を固定順で抽出。空 AI で空スライスを返す。
- `viewInput`（`view.go:44-102`）: `view.go:94` で `fmt.Sprintf("言語: %s  provider: %s", ...)` と表示。
- `nextProvider`（`model.go:151-161`）: 空リストなら current を返す。`p` キー（`update.go:161`）で呼ばれる。
- `startAnalysis`（`update.go:252-267`）: 利用可能 provider が空なら解析を開始せず `errMessage` を設定（`update.go:256-259`）。空 provider でも解析は開始されないため安全。
- 既存テスト `TestNewModelUsesSupportedConfig`（`model_test.go:99-111`）は cfg に `DefaultProvider` があるケースで provider を維持する（AI 空でも cfg 値が残る）。この挙動は変更後も維持する（未設定表示は「cfg にも DefaultProvider が無く AI も空」のときのみ）。

## スコープ

### 対象

- `internal/tui/model.go`: provider 初期値を `"claude"` → `""` に
- `internal/tui/view.go`: 表示時、provider 空なら「未設定」
- `internal/tui/model_test.go`: デフォルトテストの期待値変更 + 未設定ケースのテスト追加
- `internal/tui/update_test.go`: デフォルト provider 期待値変更 + p キー未設定挙動のテスト
- `internal/tui/view_test.go`: 表示テストの期待値変更 + 未設定表示テスト追加
- `SPEC.md`: 2.2 に未設定時の表示を追記

### 対象外

- `cmd/secrets.go` / `cmd/application.go` の provider 解決ロジック（`requireAI` 時）
- `internal/analyzer` の provider 未設定エラー文言
- `reporepo analyze` / `trending` コマンド
- provider 未設定時の `p` キー案内表示（未設定のまま維持）

## 設計

### 変更1: provider 初期値を空に（model.go:93）

```go
language, provider := "ja", ""
```

- cfg に有効な `DefaultProvider` があれば上書き（既存 `model.go:98-100` は変更なし）
- 利用可能 provider があれば、`containsProvider(available, "")` は常に false のため最初の利用可能 provider へ補正（既存 `model.go:102-105` は変更なし）
- 利用可能 provider が無く cfg にも `DefaultProvider` が無い場合のみ `provider` が `""`（＝未設定）になる

### 変更2: 表示の「未設定」化（view.go:94）

```go
providerLabel := m.provider
if providerLabel == "" {
    providerLabel = "未設定"
}
b.WriteString(fitLine(fmt.Sprintf("言語: %s  provider: %s", m.language, providerLabel), layout.width))
```

### 変更3: p キー（変更不要）

- `nextProvider` は空リストで current（空）を返すため、未設定時は `p` を押しても「未設定」のまま
- 利用可能 provider がある場合は既存どおり巡回（回帰なし）

## テストリスト

### A. 初期 provider の導出（internal/tui/model_test.go）

- [ ] cfg 無し・AI 空 → `provider == ""`（`TestNewModelUsesDefaults` / `TestNewModelUsesDefaultsWithNilConfig` を `"claude"` → `""` に変更）
- [ ] cfg に `DefaultProvider` あり・AI 空 → cfg 値を維持（`TestNewModelUsesSupportedConfig` 等は変更なし・回帰）
- [ ] AI に provider あり・cfg 無し → 最初の利用可能 provider に補正（既存挙動の回帰）
- [ ] AI 空・cfg 無しの新テスト: `provider == ""` であることを明示

### B. p キー挙動（internal/tui/update_test.go）

- [ ] AI 空・provider 空で `p` → `provider == ""` のまま（新テスト）
- [ ] AI 3 provider 設定で `p` → 固定順で巡回（`TestEmptyInputShortcutsToggleAndQuit` は回帰・変更なし）
- [ ] `TestTypingReservedAndRegularRunesGoesToTextInput` の期待値を `provider != "claude"` → `provider != ""` に変更

### C. 表示（internal/tui/view_test.go）

- [ ] AI 空 Model の入力画面で `provider: 未設定` を含む（`TestViewsContainRequiredInformation` の `"provider: claude"` → `"provider: 未設定"` に変更）
- [ ] provider 設定済み Model で `provider: claude` 等を表示する（回帰・新テスト）

### D. 回帰

- [ ] 既存の `internal/tui` / `cmd` テストが全て通る
- [ ] `gofmt -l .` / `go test ./...` / `go test -race ./...` / `go vet ./...` が成功

## 実装順序

### Step 1: テスト（red）

- `model_test.go`: デフォルト provider 期待値を `""` に変更し失敗を確認、AI 空・cfg 無しの provider 未設定テストを追加
- `view_test.go`: 期待値を `provider: 未設定` に変更し失敗を確認
- `update_test.go`: `TestTypingReservedAndRegularRunesGoesToTextInput` の期待値変更、p キー未設定挙動テスト追加

### Step 2: 実装（green）

- `model.go:93`: provider 初期値を `""` に
- `view.go:94`: 空 provider を「未設定」と表示

### Step 3: リファクタ

- `providerLabel` の導出をヘルパー関数にするか、そのままインラインでよいか判断（要件が小さいためインラインで十分な場合はそのまま）

### Step 4: SPEC 更新

- `SPEC.md:35`（2.2）に「利用可能な provider が無く default_provider も未設定の場合は『未設定』と表示し、`p` キーでは切り替わらない」を追記

### Step 5: 検証

```bash
gofmt -l .
go test ./...
go test -race ./...
go vet ./...
```

## 完了条件

- AI API key 未設定かつ `default_provider` 未設定のとき、入力画面に `provider: 未設定` と表示される
- AI API key 未設定時に `p` キーを押しても「未設定」のまま（解析の案内は従来どおり `startAnalysis` が行う）
- cfg に `default_provider` がある場合、または利用可能 provider がある場合は従来どおり provider が表示される（回帰なし）
- `SPEC.md` の該当記述が新挙動と整合する
- `gofmt` / `go test ./...` / `go test -race ./...` / `go vet ./...` が全て成功する

## 想定される変更

- `internal/tui/model.go`: provider 初期値 `"claude"` → `""`
- `internal/tui/view.go`: 空 provider の「未設定」表示
- `internal/tui/model_test.go` / `update_test.go` / `view_test.go`: 期待値変更とテスト追加
- `SPEC.md`: 2.2 の記述更新
