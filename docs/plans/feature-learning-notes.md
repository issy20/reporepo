# Plan: 学習ノート（リポジトリ単位のメモ保存）

Status: draft

## 目的

SPEC 2.13 の実装。製品の主題である学習の記録として、各リポジトリのエントリに自分のメモを保存・編集・表示できるようにする。AI 解析結果は参照用で、ユーザーが書き足すノートが学習の蓄積としての価値を持つ。

## 前提

- ノートはリポジトリ単位（言語別ではない）。`core.Entry` に `Note string` を追加する。
- 保存は既存の `store.Upsert`（非同期）で行い、お気に入りトグルの mutation 基盤を再利用する。
- `github.com/charmbracelet/bubbles/textarea` は既存依存（bubbles v1.0.0）で利用可能。
- 解析の再生成・キャッシュ更新（2.9 / 2.10）でノートを失わない。`analyzer.CloneEntry` は `Entry` を値コピーするため、`Note` は自動的に引き継がれる。
- Bubble Tea は raw mode（IXON 無効）のため、`Ctrl+S` は出力停止に干渉しない。

## スコープ

### 対象

- `internal/core/types.go`: `Entry.Note string`（`json:"note"`）追加
- `internal/tui/model.go`: `textarea` モデルと `noteEditing` 状態の追加
- `internal/tui/update.go`: 詳細画面の `n`（編集開始）、`Ctrl+S`（保存）、`Esc`（キャンセル）
- `internal/tui/view.go`: 詳細画面のノート表示・編集 UI
- `internal/tui/commands.go`: ノート保存の非同期コマンド（`mutationNote`）
- テスト: `internal/core` / `internal/tui` / `internal/analyzer`

### 対象外

- CLI からのノート編集・表示（`analyze` は参照用途のまま）
- ノートのエクスポート・検索（将来拡張）
- ノートの言語別管理（リポジトリ単位固定）

## 設計

### 変更1: データモデル（core/types.go）

```go
type Entry struct {
	FullName   string               `json:"full_name"`
	RepoMeta   *RepoMeta            `json:"repo_meta"`
	Analyses   map[string]*Analysis `json:"analyses"`
	IsFavorite bool                 `json:"is_favorite"`
	Note       string               `json:"note"`
	ViewedAt   time.Time            `json:"viewed_at"`
	CreatedAt  time.Time            `json:"created_at"`
}
```

既存 `data.json` に `note` がない場合はゼロ値（空文字）として扱い、マイグレーション不要。

### 変更2: TUIのノート編集（model.go / update.go / view.go / commands.go）

**Model への追加:**

```go
noteEditor  textarea.Model
noteEditing bool
```

`NewModel` で `textarea.New()` を初期化し、`Width` をレイアウトから設定する。`WindowSizeMsg` で幅を追随させる。

**詳細画面の操作（`updateDetail` を拡張）:**

- `noteEditing` が `false` のとき `n` → 編集モード開始。`noteEditor.SetValue(current.Note)`、フォーカス、`noteEditing = true`
- 編集モード中のキー:
  - `Ctrl+S`: 保存 → `saveNoteCmd`（非同期 `Upsert`）→ 編集モード終了
  - `Esc`: キャンセル → 編集モード終了（保存しない）
  - その他: `noteEditor.Update` へ渡す
- 編集モード以外では従来どおり（Esc で一覧へ戻る等）

**保存コマンド（commands.go）:**

`entryMutationKind` に `mutationNote` を追加し、`saveNoteCmd` は既存の mutation 基盤（`mutationRequestID` / `mutationPending`）を使って `entryMutationFinishedMsg` を返す。保存後に `reloadEntries` と `current` の更新を行う。

```go
func (m Model) saveNoteCmd(note string) tea.Cmd
```

- 保存対象: `current` を `CloneEntry` し、`Note = note` を設定して `Upsert`

**表示（view.go）:**

- 編集モード: `noteEditor.View()` とヒント `Ctrl+S: 保存  Esc: キャンセル` を表示
- 通常時: 詳細 Markdown の下にノートを表示する
  - ノートが空なら表示しない
  - ノートがあれば `## ノート` セクションとして `safeText` 適用後に追加し、ヒントに `n: ノート編集` を追記

**ノートの保持（analyzer.go）:**

`CloneEntry` は `Entry` を値コピーするため変更不要。再解析・キャッシュ更新でも `Note` は引き継がれる。確認用テストのみ追加する。

## テストリスト

### A. データモデル（core）

- [ ] `Entry` が `note` を JSON へ marshal / unmarshal する
- [ ] `note` の無い既存 JSON が空ノートとして読み込まれる（回帰）
- [ ] 既存の Entry テストが通る（回帰）

### B. ノートの保持（analyzer）

- [ ] 再解析（force）でも既存ノートが保持される
- [ ] キャッシュヒット更新でも既存ノートが保持される
- [ ] 新規解析のノートは空文字

### C. TUIの編集・保存（tui）

- [ ] `n` で編集モードに入る
- [ ] 編集モードで `Ctrl+S` を押すと `Upsert` がノート付きエントリで呼ばれる
- [ ] 編集モードで `Esc` を押すと保存せずに編集モードを抜ける
- [ ] 保存後、詳細画面にノートが表示される
- [ ] ノートが空の場合は表示されない
- [ ] ノート表示に制御文字が含まれない（`safeText`）
- [ ] 編集モード中は Esc が解析画面へ戻らない（キャンセルのみ）
- [ ] 既存TUIテストが通る（回帰）

### D. 回帰

- [ ] 既存テスト全体が通る（`go test ./...` / `-race` / `go vet`）

## 実装順序

### Step 1: テスト（red）

- core: `Entry.Note` の marshal / 既存データの回帰テスト
- analyzer: 再解析・キャッシュヒットでのノート保持テスト
- tui: `n` / `Ctrl+S` / `Esc` の状態遷移テスト

### Step 2: 実装（green）

- `types.go`: `Note` 追加
- `model.go`: `textarea` と `noteEditing` 追加
- `update.go` / `view.go` / `commands.go`: 編集・保存・表示を実装

### Step 3: 検証

```bash
gofmt -l .            # 空であること
go test ./...
go test -race ./...
go vet ./...
```

## 完了条件

- 詳細画面で `n` → `textarea` 編集 → `Ctrl+S` 保存ができ、解析結果の下にノートが表示される
- ノートはリポジトリ単位で `data.json` に保存され、再解析・キャッシュ更新後も保持される
- ノート表示に制御文字が含まれない
- `gofmt` / `go test ./...` / `go test -race ./...` / `go vet ./...` が全て成功する

## 想定される変更

- `internal/core/types.go`: `Entry.Note` 追加
- `internal/tui/model.go`: `textarea` / `noteEditing` 追加、初期化
- `internal/tui/update.go`: `n` / `Ctrl+S` / `Esc` の分岐
- `internal/tui/view.go`: ノート表示・編集 UI
- `internal/tui/commands.go`: `mutationNote` と `saveNoteCmd`
- テスト: `internal/core` / `internal/tui` / `internal/analyzer` に追加

## worktree

- branch: `feature-learning-notes`
- worktree path: `/Users/issy20/ccplayground/reporepo/feature-learning-notes`

理由: SPEC 2.13 の新機能であり、`internal/core` の1フィールド追加と TUI の編集UI に収まる。データモデル・保存・表示の順に TDD で進める。
