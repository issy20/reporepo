# Plan: data.json の並行アクセス整合性（ファイルロックと削除のストア層移行）

Status: プロセス間（完了）/ プロセス内ゴルーチン競合（未実装）

## 目的

SPEC 3章 非機能要件「並行アクセスの整合性」の実装。TUI（`run`）と CLI（`analyze`）が同一の `data.json` を共有しており、複数プロセスが同時に書き込むと `lost update`（相手の追加分が消える）が発生する。`internal/store` にファイルロックを導入して read-modify-write を逐次化し、TUI の削除をストア層の `Delete` へ移行する。

> 注: 下記の **プロセス間（Phase 1）は実装完了**。追加レビューで判明した **プロセス内ゴルーチン競合（Phase 2）** を本計画に追記する（「Phase 2: プロセス内ゴルーチン競合」の章を参照）。

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

---

## Phase 2: プロセス内ゴルーチン競合（更新喪失の残存リスク）

### 目的

Phase 1（flock）は**プロセス間**の逐次化を解決した。しかし追加レビューにより、**同一プロセス内**でも並行ゴルーチンが無ロックで `Load → 変更 → Save` するストアを書き換え得ることが判明した。flock はプロセス内の複数 goroutine からのアクセスも逐次化するが、**「古いクローンによる上書き」**は goroutine の順序だけでは防げないため、TUI / analyzer 層でガードする。

### 発火経路（前回レビューより）

- ノート保存の `mutationPending` 中に `r` / `l`（解析）が実行できる（`update.go:235` の `mutationPending` ガードがノート以外に効かない）
- 解析を `esc` キャンセル（`update.go:180-188`）した直後に `f` / `d` が実行でき、実行中の `analyzer.Analyze` goroutine がキャンセル済み context でも `store.Upsert` を続行し得る
- `CloneEntry`（`analyzer.go`）で作った**古い状態のクローン**が、再生成済みの解析や更新済み `RepoMeta` を上書きし得る

### スコープ（Phase 2）

#### 対象

- `internal/tui/update.go`: 解析・削除・お気に入りの `mutationPending` ガードを全 mutation へ統一し、並行開始を防ぐ
- `internal/analyzer/analyzer.go`: キャンセル済み context での `store.Upsert` 継続を防ぐ
- `internal/tui/commands.go`: ノート保存・削除・お気に入りの書き込みを、解析中の書き込みと直列化する
- テスト: `internal/tui` / `internal/analyzer` の並行・キャンセル・上書きテスト

#### 対象外

- Phase 1 の flock（`internal/store`）の変更
- ストア層でのバージョニング（ETag / 楽観ロック）。現在は単一エントリ操作のため不要と判断

### 設計（Phase 2）

#### 変更1: mutation の排他を全操作に統一

`mutationPending` は現在ノート保存（`updateNoteEditing`）以外の開始をガードしない。解析は `stateLoading` でブロックされるが、ノート保存中は `stateDetail` のままなので `r` / `l` が通る。次の方針で統一する。

- 解析の開始（`startAnalysis`）を `mutationPending` 中はブロックする
- ノート・お気に入り・削除の開始も `mutationPending` で直列化（既存を維持）
- ノート保存中（`noteEditing` + 保存実行中）は `r` / `l` / `f` を無効化し、完了通知（`entryMutationFinishedMsg`）後に解除

これにより、同一プロセス内で並行する `Upsert` / `Delete` の発生自体を防ぐ。

#### 変更2: キャンセル済み解析の書き込み抑止

`analyzer.Analyze` は context キャンセル後も `store.Upsert` を呼ぶ可能性がある（`contextError` チェックの直後に書き込み位置まで進む経路）。書き込み直前に `contextError` を再確認するガードを、保存前の最終地点に追加する。キャンセル時は「保存しないで終了」とし、部分的な書き込みを防ぐ。

#### 変更3: 古いクローンの上書き防止

`CloneEntry` + `Upsert` の read-modify-write は、クローン作成後に他 goroutine が保存した新状態を失わせる。Phase 2 ではTUI層での排他（変更1）により同時実行を防ぐのが主眼。加えて、解析完了時の `reloadEntries` で常に最新状態を読み直し、UI が古い状態を表示・保持しないようにする。

### テストリスト（Phase 2）

- [ ] ノート保存中（`mutationPending`）に `r` / `l` を押しても解析が開始されない
- [ ] `esc` キャンセル直後の `f` / `d` が、実行中の解析書き込みと競合しない（並行テスト）
- [ ] キャンセル済み context の解析が `store.Upsert` を呼ばない
- [ ] ノート保存と解析が並行して起きない（`mutationPending` ガード）
- [ ] 既存 TUI / analyzer テストが通る（回帰）

### 実装順序（Phase 2）

1. TUI の `mutationPending` 統一（テスト → red → green）
2. analyzer の保存前 `contextError` 再確認（テスト → red → green）
3. `reloadEntries` による最新状態の読み直し確認
4. 検証: `gofmt -l .` / `go test ./...` / `go test -race ./...` / `go vet ./...`

### 完了条件（Phase 2）

- 同一プロセス内で並行する `store.Upsert` / `Delete` が発生しない
- キャンセル済み解析がストアを書き換えない
- 古いクローンによる再生成済み解析・`RepoMeta` の上書きが起きない
- `gofmt` / `go test ./...` / `go test -race ./...` / `go vet ./...` が全て成功する

理由: SPEC 3章 非機能要件の実装であり、`internal/store` を中心とした改修。依存追加（`gofrs/flock`）と TUI の削除移行を含むため、専用 worktree で進める。
