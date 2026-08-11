# Plan: 疑似Trending一覧のTUI導線（Phase 2）

Status: draft

## 目的

SPEC 2.12 の Phase 2。入力画面に `t` キーで Trending 一覧を表示する導線を追加する。一覧から Enter で選択した repo を既存の解析パイプライン（キャッシュ・鮮度・保存）へ流し込み、解析結果は履歴・お気に入りと同じ扱いにする。

Phase 1（CLI `reporepo trending`）の一覧キャッシュは `cmd` パッケージに実装済み。TUI は `internal/tui` にあり `cmd` を参照できないため、キャッシュロジックを `internal/trendingcache` へ抽出して CLI / TUI で共有する（合意済み）。

## 前提

- `clients.SearchTrending` / `TrendingQuery` / `TrendingRepo` / `ErrTrendingRateLimited` は Phase 1 で実装済み。
- 検索結果の repo 名・説明は攻撃者制御データ。表示は `view.go` の既存 `safeText` / `safeMarkdownText` でサニタイズする。
- TUI のクエリは固定（`since=week`、`minStars=50`、言語絞り込みなし）。CLI の既定フラグと同じ `trendingCacheKey("week", 50, "")` になり、キャッシュファイルを共有できる。
- `Dependencies.TrendingCachePath` が空の場合はファイルキャッシュを使わず毎回取得する（既存 TUI テストの互換を保つ）。

## スコープ

### 対象

- `internal/trendingcache/cache.go`: Phase 1 の `cmd/trending_cache.go` を移設（`TrendingCache` の公開API化）
- `cmd/trending_cache.go` / `cmd/trending_cache_test.go`: 削除（`internal/trendingcache` へ移設）
- `cmd/trending.go` / `cmd/trending_test.go`: `internal/trendingcache` 利用へ変更
- `internal/tui/model.go` / `update.go` / `view.go` / `commands.go`: `stateTrending` 追加、`t` キー導線
- `internal/tui/model_test.go` / `update_test.go` / `view_test.go` / `commands_test.go`: テスト追加
- `cmd/application.go`: `runApplicationWith` で `tui.Dependencies.TrendingCachePath` を渡す

### 対象外

- Trending 一覧での言語絞り込み・`--since` 変更（TUI では固定クエリのみ）
- 一覧からのバッチ解析
- 一覧画面での詳細プレビュー

## 設計

### 変更1: キャッシュの internal 化（internal/trendingcache）

Phase 1 の `cmd/trending_cache.go` をそのまま移設する。`cmd` 固有の参照は持たない。

```go
// internal/trendingcache/cache.go
const DefaultTTL = 6 * time.Hour

type Cache struct {
	Version int
	Entries map[string]entry
}

func Path(dataPath string) string                       // <dataPath dir>/trending-cache.json
func Key(since string, minStars int, language string) string // since|minStars|language
func Load(path string) *Cache                           // 壊れた/無いファイルは空キャッシュ
func (c *Cache) Fresh(key string, now time.Time, ttl time.Duration) ([]clients.TrendingRepo, bool)
func (c *Cache) Any(key string) ([]clients.TrendingRepo, bool) // レート制限フォールバック用
func (c *Cache) Set(key string, repos []clients.TrendingRepo, now time.Time)
func Save(path string, cache *Cache) error              // 一時ファイル + rename + 0600
```

- `cmd/trending.go` の `trendingCachePath` / `trendingCacheKey` / `loadTrendingCache` / `saveTrendingCache` 参照を `trendingcache.Path` / `trendingcache.Key` / `trendingcache.Load` / `trendingcache.Save` へ置換する。`cmd/trending.go` のキャッシュ扱いロジック（fresh → miss → レート制限フォールバック）は不変。
- テストは `cmd/trending_cache_test.go` を `internal/trendingcache/cache_test.go` へ移設し、パッケージ名を `trendingcache` に合わせる。

### 変更2: TUI の `t` キー導線

**状態。** `screenState` に `stateTrending` を追加する。

```go
const (
	stateInput screenState = iota
	stateLoading
	stateDetail
	stateTrending
)
```

**Model フィールド。** 一覧の選択状態を保持する。

```go
trendingRepos     []clients.TrendingRepo
trendingSelected  int
trendingErr       string
trendingLoading   bool
trendingRequestID uint64
```

**Dependencies。** キャッシュパスを追加する。空ならキャッシュなし。

```go
type Dependencies struct {
	Store             entryStore
	GitHub            clients.GitHubClient
	AI                map[string]clients.AIClient
	Now               func() time.Time
	Renderer          markdownRenderer
	TrendingCachePath string
}
```

**起動。** `stateInput` で入力値が空のときに `t` キーで `startTrending` を呼ぶ。リクエストIDを採番し、状態を `stateTrending` にして非同期取得を開始する。

```go
func (m Model) startTrending() (tea.Model, tea.Cmd) {
	// github nil ならエラーメッセージで戻る
	m.state = stateTrending
	m.trendingRepos, m.trendingSelected, m.trendingErr = nil, 0, ""
	m.trendingLoading = true
	m.trendingRequestID++
	requestID := m.trendingRequestID
	return m, m.trendingCmd(requestID)
}
```

**非同期コマンド（commands.go）。** キャッシュ確認 → 取得 → 保存を1コマンドにまとめる。レート制限時はキャッシュがあれば `stale=true` で返す。

```go
type trendingLoadedMsg struct {
	requestID uint64
	repos     []clients.TrendingRepo
	stale     bool
}
type trendingFailedMsg struct {
	requestID uint64
	err       error
}
```

`trendingCmd` の流れ:
1. `now = m.now()`、クエリは `TrendingQuery{CreatedAfter: now.AddDate(0,0,-7), MinStars: 50}`
2. `TrendingCachePath != ""` なら `trendingcache.Load` → `Fresh(key, now, DefaultTTL)` ヒットで即返す
3. ミスなら `m.github.SearchTrending` → 成功で `Set` + `Save`（保存失敗は無視 or 警告にしない）→ `trendingLoadedMsg`
4. `ErrTrendingRateLimited` なら `Any(key)` があれば `stale=true` で返し、なければ `trendingFailedMsg`

**状態遷移（update.go）。** `trendingLoadedMsg` / `trendingFailedMsg` は `requestID` 不一致なら無視する（既存の分析系メッセージと同様）。

- `trendingLoadedMsg`: `trendingRepos` 更新、`trendingLoading=false`、`stale` なら `trendingErr` に「キャッシュ表示」の案内
- `trendingFailedMsg`: `trendingErr` に安全なエラー、`trendingLoading=false`
- `stateTrending` のキー操作（`updateTrending`）:
  - `enter`: `trendingRepos[trendingSelected].FullName` を `startAnalysis(fullName, false)` へ（既存パイプライン）
  - `up` / `down`: 選択移動
  - `t`: 再取得（`startTrending`）
  - `esc`: `trendingRequestID++` して `stateInput` へ戻る（進行中の結果を無効化）
- `tea.KeyMsg` のディスパッチに `case stateTrending: return m.updateTrending(msg)` を追加
- スピナーのtick条件を `stateLoading || m.trendingLoading` へ拡張

**view（view.go）。** `viewTrending` を追加し `View()` の switch に組み込む。

- ヘッダ: 「Trending（直近の作成・急上昇）」＋キャッシュ案内
- ローディング中: `m.spinner.View() + " 取得しています…"`
- 一覧: `> owner/repo ⭐ 123  説明  Go` の行（`safeText`、選択行は `selectedStyle`）。0件は「該当するリポジトリはありません」
- エラー表示（`trendingErr`）
- フッタ: 「Enter: 開く ↑↓: 選択 t: 再取得 Esc: 戻る」

**application.go。** `runApplicationWith` の `tui.Dependencies` へキャッシュパスを渡す。

```go
tuiDeps := tui.Dependencies{
	Store:             rt.store,
	GitHub:            rt.github,
	AI:                rt.ai,
	Now:               time.Now,
	TrendingCachePath: trendingcache.Path(rt.dataPath),
}
```

## テストリスト

### A. キャッシュ internal 化（internal/trendingcache/cache_test.go、回帰）

- [ ] Phase 1 の `cmd/trending_cache_test.go` の全テストを移設して通る
- [ ] `cmd/trending_test.go` が internal 化後も通る（回帰）
- [ ] `cmd/trending_cache.go` / `cmd/trending_cache_test.go` が存在しない

### B. TUI `t` キー導線（internal/tui）

- [ ] 入力画面で `t` を押すと `stateTrending` に遷移し取得中はローディング表示
- [ ] 取得完了で一覧が `trendingRepos` に設定され、`View()` に一覧が現れる
- [ ] `up` / `down` で選択が移動する
- [ ] `enter` で選択した repo の解析が開始され、成功で詳細表示・ストア保存される
- [ ] `esc` で入力画面へ戻る（進行中の取得結果は無視される）
- [ ] 取得失敗時はエラーメッセージが表示され、`esc` で戻れる
- [ ] レート制限時にキャッシュがなければ安全なエラー案内を表示
- [ ] `TrendingCachePath` 指定時にキャッシュが fresh なら `SearchTrending` を呼ばない
- [ ] レート制限時にキャッシュがあればキャッシュ一覧を表示し、キャッシュ由来の案内を出す
- [ ] 入力値が非空のときの `t` は textinput へ渡される
- [ ] 一覧の repo 名・説明の制御文字・ANSI が除去される
- [ ] 0件のときは「該当するリポジトリはありません」
- [ ] `TrendingCachePath` 未指定でも動作する（キャッシュなし）
- [ ] 既存 TUI テスト全体が通る（回帰）

### C. 配線（cmd/application_test.go）

- [ ] `runApplicationWith` が `tui.Dependencies.TrendingCachePath` へ dataPath 由来のキャッシュパスを渡す

## 実装順序

### Step 1: キャッシュ internal 化（red → green → リファクタ）

- `internal/trendingcache/cache_test.go` を移設し、`internal/trendingcache/cache.go` を実装
- `cmd/trending.go` / `cmd/trending_test.go` を `trendingcache` 参照へ変更
- `cmd/trending_cache.go` / `cmd/trending_cache_test.go` を削除

### Step 2: TUI `t` キー導線（red → green）

- `internal/tui/update_test.go` / `view_test.go` / `commands_test.go` にテスト追加
- `model.go` / `update.go` / `view.go` / `commands.go` を実装

### Step 3: 配線（green）

- `cmd/application.go` の `runApplicationWith` に `TrendingCachePath` を追加
- `cmd/application_test.go` に配線テスト追加

### Step 4: 検証

```bash
gofmt -l .            # 空であること
go test ./...
go test -race ./...
go vet ./...
```

## 完了条件

- 入力画面で `t` を押すと Trending 一覧が表示され、`enter` で選択した repo が既存パイプラインで解析・保存される
- CLI と TUI が同一の `trending-cache.json` を共有し、6時間キャッシュが機能する
- レート制限時はキャッシュ表示または再試行の案内になる
- 表示に制御文字・ANSI・secret が含まれない
- `gofmt` / `go test ./...` / `go test -race ./...` / `go vet ./...` が全て成功する

## 想定される変更

- `internal/trendingcache/cache.go` / `cache_test.go`: 新規（`cmd/trending_cache.go` の移設）
- `cmd/trending_cache.go` / `cmd/trending_cache_test.go`: 削除
- `cmd/trending.go` / `cmd/trending_test.go`: `internal/trendingcache` 参照へ変更
- `internal/tui/model.go` / `update.go` / `view.go` / `commands.go`: `stateTrending` と `t` キー導線
- `internal/tui/model_test.go` / `update_test.go` / `view_test.go` / `commands_test.go`: テスト追加
- `cmd/application.go` / `cmd/application_test.go`: `TrendingCachePath` 配線

## worktree

Phase 2 は Phase 1 から分離し、別worktreeで実施する。Phase 1 のキャッシュファイル（`cmd/trending_cache.go`）を削除・移設するため、Phase 1 を先に確定させてから着手する。

- 手順
  1. 現worktree（`feature-trending`）で Phase 1 をコミット・マージする
  2. マージ済み main から `feature-trending-tui` ブランチの新worktreeを作成する
  3. 新worktreeで本計画を実施する
- 想定する新worktree
  - branch: `feature-trending-tui`
  - path: `/Users/issy20/ccplayground/reporepo/feature-trending-tui`

理由: Phase 2 は Phase 1 の実装（`SearchTrending`・キャッシュ）に依存し、かつ Phase 1 由来ファイルの削除を伴う。同一worktreeで進めると Phase 1 と Phase 2 が同一変更単位に混在し、レビュー・リバートが不明瞭になる。リポジトリの機能別ブランチ分割の慣習にも従う。
