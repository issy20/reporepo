# Plan: 詳細画面でのミューテーション失敗表示

Status: draft

## 目的

詳細画面（`stateDetail`）でミューテーション失敗（ノート保存失敗・お気に入り失敗・削除失敗）がエラーとして表示されないバグを修正する。ミューテーション失敗は `m.errMessage` に格納されるが（`update.go:64`）、`viewDetail` は `errMessage` を描画しない（`view.go:173-193`）。特に Ctrl+S（ノート保存）失敗はユーザーに無表示で静かに失われる。

## 前提

- `errMessage` は現在 `viewInput`（`view.go:97-99`）でのみ描画される。`viewDetail` / `viewTrending` は描画しない。
- ミューテーション失敗は `entryMutationFinishedMsg` ハンドラ（`update.go:57-72`）で `m.errMessage = msg.err.Error()` と設定され、`state` は変更されない（`stateDetail` のまま）。
- 失敗時は `reloadEntries` を実行しないため、一覧側の表示は変えない。詳細画面でエラーを表示する必要がある。
- エラー表示は既存の `errorStyle` と `safeText` を使い、TTY / plain の表示ルール（SPEC 2.8）に従う。

## スコープ

### 対象

- `internal/tui/view.go`: `viewDetail`（および必要なら `viewTrending`）に `errMessage` の描画を追加
- `internal/tui/view_test.go`: 詳細画面でのエラー表示テスト追加

### 対象外

- ミューテーション失敗の検出ロジック（`update.go` のハンドラは変更しない）
- `errMessage` のクリア条件（既存の解析成功・開始時クリア等は変更しない）
- ノート保存の並行ガード（Phase 2 の別計画で対応）

## 設計

### 変更1: viewDetail へのエラー表示追加

`viewDetail` で `errMessage` があれば、詳細コンテンツとヒントの間にエラーを表示する。`viewInput` と同じ `errorStyle` + `safeText` を使う。

```go
func (m Model) viewDetail() string {
	width := newLayout(m.width, m.height).width
	if m.current == nil {
		return fitLine("解析結果がありません", width) + "\n\n" + fitLine("Esc: 戻る", width)
	}
	var b strings.Builder
	if m.noteEditing {
		b.WriteString(m.noteEditor.View())
		b.WriteByte('\n')
		b.WriteString(fitLine(dimStyle.Render("Ctrl+S: 保存  Esc: キャンセル"), width))
		return b.String()
	}
	b.WriteString(m.viewport.View())
	for _, warning := range m.warnings {
		b.WriteByte('\n')
		b.WriteString(fitLine(warningStyle.Render(safeText(warning)), width))
	}
	b.WriteByte('\n')
	if m.errMessage != "" {
		b.WriteString(fitLine(errorStyle.Render(safeText(m.errMessage)), width))
		b.WriteByte('\n')
	}
	b.WriteString(fitLine(dimStyle.Render("Esc: 戻る  ↑↓/PgUp/PgDn: スクロール  l: 言語  f: お気に入り  r: 再生成  n: ノート編集"), width))
	return b.String()
}
```

- ミューテーション失敗時に `errMessage` が表示され、成功時は表示されない
- 既存の回帰テスト（`view_test.go:42` 等）が引き続き通る

## テストリスト

### A. 詳細画面でのエラー表示（view_test.go）

- [ ] `stateDetail` で `errMessage` が非空のとき、`viewDetail` にエラーテキストが含まれる
- [ ] エラー内の制御文字が除去される（`safeText`）
- [ ] `errMessage` が空のときはエラーが表示されない（回帰）
- [ ] ノート編集モード中はエラーが表示されず、編集ビューのみが表示される（回帰）

### B. ミューテーション失敗の統合（update + view）

- [ ] ノート保存失敗（Ctrl+S）時に `n errMessage` が設定され、`viewDetail` に表示される
- [ ] お気に入り失敗・削除失敗時も同様に表示される
- [ ] 成功時は `errMessage` が表示されない

### C. 回帰

- [ ] 既存の TUI テストが全て通る（`view_test.go` / `update_test.go` / `model_test.go`）
- [ ] `gofmt -l .` / `go test ./...` / `go test -race ./...` / `go vet ./...` が成功

## 実装順序

### Step 1: テスト（red）

- `view_test.go`: `stateDetail` + `errMessage` 非空で `viewDetail` がエラーを含むテストを書いて失敗を確認
- `update_test.go`: ノート保存失敗（Ctrl+S → `entryMutationFinishedMsg` 失敗）で `errMessage` が設定されることを確認（既存のストアエラー経路を利用）

### Step 2: 実装（green）

- `view.go`: `viewDetail` に `errMessage` 描画を追加

### Step 3: 検証

```bash
gofmt -l .            # 空であること
go test ./...
go test -race ./...
go vet ./...
```

## 完了条件

- 詳細画面でミューテーション失敗（ノート保存・お気に入り・削除）がエラーとして表示される
- 成功時・`errMessage` 空のときは表示されない
- エラーテキストから制御文字が除去される
- `gofmt` / `go test ./...` / `go test -race ./...` / `go vet ./...` が全て成功する

## 想定される変更

- `internal/tui/view.go`: `viewDetail` への `errMessage` 描画追加
- `internal/tui/view_test.go`: 詳細画面エラー表示テスト追加
- `internal/tui/update_test.go`: ノート保存失敗の表示テスト追加

## worktree

- branch: `fix-detail-error-display`
- worktree path: `/Users/issy20/ccplayground/reporepo/fix-detail-error-display`

理由: この修正は独立した小さなバグ修正であり、Phase 2（プロセス内ゴルーチン競合）の実装とは独立に扱えるため、専用 worktree で進める。
