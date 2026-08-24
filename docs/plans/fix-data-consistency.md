# Plan: data.json の並行アクセス整合性（ファイルロックと削除のストア層移行）

Status: draft

## 目的

SPEC 3章 非機能要件「並行アクセスの整合性」の実装。TUI（`run`）と CLI（`analyze`）が同一の `data.json` を共有しており、複数プロセスが同時に書き込むと `lost update`（相手の追加分が消える）が発生する。`internal/store` にファイルロックを導入して read-modify-write を逐次化し、TUI の削除をストア層の `Delete` へ移行する。

## 前提

- `data.json` 自体は一時ファイル + rename で保存され、都度 inode が入れ替わるため、`data.json` にロックを取ると古い inode を掴んだままになる。ロックは**別ファイル `data.json.lock`**（rename しない固定 inode）に対して取得する。
- flock はカーネルがプロセス終了時に自動解放するため、stale ロックは残らない。PID ファイル方式ではない。
- クロスプラットフォーム対応のため、`gofrs/flock`（Unix: flock / Windows: LockFileEx）を利用する。go.mod への追加を伴う。
- ロックは「Load → マージ → Save」全体を1つの critical section にする。`Save` だけをロックしても Load との間に別プロセスが割り込むため不十分。

## スコープ

### 対象

- `internal/store/store.go`: `Lock` 追加、`Upsert` / `Delete` / `Save` をロック内で行う
- `internal/store/store_test.go`: 並行アクセス・削除の直列化テスト
- `internal/tui/update.go`: `deleteSelected` を `store.Delete(fullName)` へ移行
- `internal/tui/commands.go` / `update.go`: 削除の mutation 基盤を `Delete` 経由に更新
- テスト: `internal/tui` の削除関連テスト更新
- `go.mod` / `go.sum`: `gofrs/flock` 追加

### 対象外

- 複数エントリを跨ぐトランザクション（単一エントリ操作のみのため不要）
- マージ型永続化や SQLite への移行
- `trending-cache.json` の並行アクセス（現在は単一ユーザー・低頻度。必要なら将来対応）
- `internal/analyzer` の変更（`store.Upsert` はインターフェース経由のため変更不要）

## 設計

### 変更1: ロックの導入（store.go）

`Store` に操作対象の `dataPath` からロックファイルパスを導出して保持させる。

```go
type Store struct {
	filepath string
	lockPath string
}

func NewStore(filepath string) *Store {
	return &Store{filepath: filepath, lockPath: filepath + ".lock"}
}
```

ロックは read-modify-write 全体を囲む。内部メソッドをロックなしのプライベート実装と、ロック付きの公開メソッドに分ける。

```go
func (s *Store) withLock(fn func() error) error {
	lock, err := flock.Acquire(s.lockPath, lockTimeout)
	if err != nil {
		return err
	}
	defer lock.Release()
	return fn()
}

func (s *Store) Upsert(entry *core.Entry) error {
	if entry == nil {
		return errors.New("cannot upsert nil entry")
	}
	return s.withLock(func() error {
		entries, err := s.load()
		...
		return s.save(entries)
	})
}
```

- `lockTimeout`: 短時間（例: 5 秒）。タイムアウト時は「他のプロセスが保存中です。再実行してください」という安全なエラー
- `load` / `save` は既存の `Load` / `Save` をプライベート化し、公開メソッドは `withLock` を呼ぶ形にする
- `Save` の公開シグネチャ（`Save([]*core.Entry) error`）は lock 内呼び出しに変更（TUI の削除がこれを使わなくなるため、`Delete` へ移行）

### 変更2: 削除のストア層移行（store.go / update.go）

`Delete(fullName string) error` を追加する。「Load → fullName と一致しないエントリのみ保持 → Save」という read-modify-write をロック内で行う。

```go
func (s *Store) Delete(fullName string) error {
	return s.withLock(func() error {
		entries, err := s.load()
		if err != nil {
			return err
		}
		filtered := make([]*core.Entry, 0, len(entries))
		for _, e := range entries {
			if e != nil && !strings.EqualFold(e.FullName, fullName) {
				filtered = append(filtered, e)
			}
		}
		return s.save(filtered)
	})
}
```

TUI の `deleteSelected`（`update.go`）は、メモリスナップショットで `store.Save(entries)` する現状をやめ、`store.Delete(target.FullName)` の非同期コマンドへ置き換える。これにより他プロセスが追加したエントリが失われない。`entryStore` インターフェース（`internal/tui/model.go`）に `Delete` を追加し、TUI テストの fake（`fakeStore` / `recordingStore`）を追随させる。

## テストリスト

### A. 並行整合性（store_test.go）

- [ ] N 個の並行 `Upsert`（それぞれ別 FullName）で全 N 件が残る（ロックなしでは間欠的に失われる）
- [ ] 並行 `Upsert` と `Delete` の直列化（削除している間に追加した分が消えない）
- [ ] ロック取得にタイムアウトした場合、安全なエラーを返す
- [ ] `Delete` が一致するエントリだけを削除する（大文字小文字を無視）
- [ ] 既存の `Upsert` / `SaveAndLoad` が通る（回帰）

### B. TUIの削除（tui）

- [ ] `d` キーの削除が `store.Delete` を呼び、追加分を失わない
- [ ] 削除後、表示一覧が更新される（回帰）
- [ ] 既存TUIテストが通る（回帰）

### C. 回帰

- [ ] `gofmt` / `go test ./...` / `go test -race ./...` / `go vet ./...` が成功
- [ ] `go mod tidy` 後に go.mod / go.sum が整合

## 実装順序

### Step 1: テスト（red）

- store_test.go: 並行 `Upsert` / `Delete` の直列化テスト、ロックタイムアウトテストを書いて失敗を確認
- tui: `d` キーが `store.Delete` を呼ぶテスト

### Step 2: 実装（green）

- go.mod へ `gofrs/flock` 追加（`go get`）
- store.go: `withLock` / `Load`→`load` / `Save`→`save` / `Upsert` / `Delete` を実装し、公開 `Load` は lock 内読み取りに
- update.go: `deleteSelected` を `store.Delete` 経由の非同期コマンドへ

### Step 3: 検証

```bash
go mod tidy
gofmt -l .            # 空であること
go test ./...
go test -race ./...
go vet ./...
```

## 完了条件

- 複数プロセス（TUI + CLI / 並列 CLI）が同時に書き込んでも `lost update` が起きない
- 削除は `store.Delete(fullName)` としてストア層のロック内で行われ、他プロセスの追加分を失わない
- ロックのタイムアウト・flock 失敗が安全なエラーで返る
- `gofmt` / `go test ./...` / `go test -race ./...` / `go vet ./...` が全て成功する

## 想定される変更

- `internal/store/store.go`: `lockPath` / `withLock` / `Delete` 追加、`Upsert` / `Save` / `Load` のロック化
- `internal/store/store_test.go`: 並行整合性・削除テスト追加
- `internal/tui/update.go`: `deleteSelected` の `store.Delete` 化
- `internal/tui/model.go` / `commands.go`: `entryStore` へ `Delete` 追加、mutation 基盤更新
- `internal/tui/` テスト: fake の `Delete` 追加
- `go.mod` / `go.sum`: `gofrs/flock` 追加

## worktree

- branch: `fix-data-consistency`
- worktree path: `/Users/issy20/ccplayground/reporepo/fix-data-consistency`

理由: SPEC 3章 非機能要件の実装であり、`internal/store` を中心とした改修。依存追加（`gofrs/flock`）と TUI の削除移行を含むため、専用 worktree で進める。
