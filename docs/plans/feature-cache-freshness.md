# Plan: キャッシュの鮮度管理（メタ情報の定期再取得と古い解析の表示）

Status: draft

## 目的

一度解析したリポジトリの GitHub 由来データ（説明・スター数・README・コード）が永久に古いまま表示される問題を解消する。開くたびに自動で鮮度を維持し、古い解析は「更新前のもの」と分かるようにする。AI 解析は費用がかかるため自動再生成せず、案内の表示に留める（SPEC 2.10）。

## 前提と技術選定

- 共有解析パイプライン `internal/analyzer` が `feature-code-analysis` で抽出済みであること。
- 古さの判定はフィールドを持たず導出する: `analysis.CreatedAt < repo.UpdatedAt` なら古い解析（SPEC 2.10）。これにより既存 `data.json` の移行と `Stale` フィールド追加を不要にする。
- メタ再取得は `GET /repos/{owner}/{repo}` の 1 リクエストで行い、レート制限への負荷を抑える（解析全体の上限10回に加えない・増やさない）。
- `refreshInterval` は既定 `7日` の定数。テストでは注入で短縮する。config 化はしない。
- リフレッシュ失敗は閲覧を失敗にしない（警告を返す）。

## スコープ

### 対象

- `RepoMeta.FetchedAt` の追加
- GitHub client への `FetchRepositoryMeta`（メタのみ取得）追加
- `Analyzer` のキャッシュヒット時リフレッシュとメタ更新（`Languages` 維持）
- 解析結果に警告を持たせる `Result` 構造体の導入
- 詳細画面の取得・解析日時表示と stale 案内
- 一覧画面の `◌` マーク
- SPEC 2.10 に沿ったテスト・README の更新

### 対象外

- AI の自動再生成（`r` での手動再生成のみ）
- README・コードの定期再取得（再生成時のフル取得のみ）
- `refreshInterval` の設定ウィザード・`config.json` 化
- `Languages` の部分更新（次回フル取得まで古いままを許容）
- 一覧表示時の GitHub API 呼び出し（開いたときのみ再取得）

## 設計

### データモデル

`core.RepoMeta` に追加:

```go
FetchedAt time.Time `json:"fetched_at"` // 最後に GitHub から取得した日時
```

`core.Analysis` に導出判定を追加:

```go
// IsStale は解析がリポジトリの最終更新より前のものかを返す。
func (a *Analysis) IsStale(meta *RepoMeta) bool
```

- `a == nil || meta == nil` なら false。
- `!a.CreatedAt.Before(meta.UpdatedAt)` なら false（ゼロ UpdatedAt も false）。
- それ以外は true。

既存 `data.json` には `FetchedAt` がないためゼロ値として扱う。ゼロは「要更新」とみなす（移行不要）。

### GitHub client

```go
// FetchRepositoryMeta は /repos/{owner}/{repo} の1リクエストでメタ情報のみ返す。
FetchRepositoryMeta(ctx context.Context, owner, repo string) (*core.RepoMeta, error)
```

- `githubRepoMeta` のパースを `FetchRepository` と共有するヘルパーへ分離する。
- 返る `RepoMeta.Languages` は空（呼び出し側で既存値を維持）。
- 404・レート制限・その他エラーの変換は既存の `handleResponseError` を再利用する。

### Analyzer のリフレッシュ

`Analyzer` に `refreshInterval time.Duration` を追加し、`Analyze` のキャッシュヒット経路へ鮮度チェックを入れる。

```go
type Result struct {
    Entry    *core.Entry
    Warnings []string // 閲覧を妨げない警告（リフレッシュ失敗等）
}
func (a *Analyzer) Analyze(ctx context.Context, input, language, provider string, force bool) (*Result, error)
```

**キャッシュヒット時（`force=false`・入力一致）:**

```
if needsRefresh(entry, now, interval):
    fresh, err := a.github.FetchRepositoryMeta(ctx, owner, repo)
    if err != nil:
        ctx cancel ならエラー / それ以外は Warnings に追加しキャッシュ表示
    else:
        if fresh.UpdatedAt != entry.RepoMeta.UpdatedAt:
            entry.RepoMeta = mergeMeta(entry.RepoMeta, fresh)   // Languages 維持
        entry.RepoMeta.FetchedAt = now()
        a.store.Upsert(entry)
return Result{Entry: entry(updated), Warnings: warnings}
```

`needsRefresh`:

```go
func needsRefresh(m *core.RepoMeta, now time.Time, interval time.Duration) bool {
    if m == nil || m.FetchedAt.IsZero() { return true }
    return now.Sub(m.FetchedAt) >= interval
}
```

`mergeMeta` は fresh のスカラー項目（FullName・Description・Stars・Forks・Language・Topics・License・URL・UpdatedAt）を old へ上書きし、`Languages` と `FetchedAt` は old のまま維持する。

- 更新判定は「refreshInterval 超」ごとの1回のみで、AI・README・コードは再取得しない。
- `force=true`（`r` 再生成）は従来どおりフル取得 + AI 生成で、`FetchedAt` を更新する。
- キャッシュミス時もフル取得で `FetchedAt` を更新する（既存動作）。

### 表示（TUI）

- **詳細ヘッダ**: `取得: {FetchedAt からの相対} / 解析: {analysis.CreatedAt からの相対}` を追加。
- **stale 案内**: 表示中の解析が `IsStale` なら「この解析はリポジトリの更新前のものです（`r` で再生成）」を詳細画面へ追加。
- **一覧**: エントリに古い解析（いずれかの `Analysis.IsStale`）があれば `◌` を付与。未確認のエントリへ推測マークは付けない（GitHub API を呼ばない）。
- **警告**: `Result.Warnings` を入力画面または詳細画面へ表示。

```go
// tui/view.go のヘルパー
func entryHasStaleAnalysis(entry *core.Entry) bool
func relativeDay(t, now time.Time) string
```

## TDDテストリスト

実装時は一度に一件だけ選び、red → green → refactor を繰り返す。

### A. データモデルと導出

- [ ] `RepoMeta.FetchedAt` が marshal / unmarshal される
- [ ] `CreatedAt < UpdatedAt` で `IsStale` が true
- [ ] それ以外（同日以降・ゼロ UpdatedAt・nil）で false
- [ ] 既存 JSON のゼロ FetchedAt が移行不要で読み込める

### B. GitHubClient.FetchRepositoryMeta

- [ ] `/repos/{owner}/{repo}` の1リクエストだけを送る
- [ ] メタ情報（スター・説明・UpdatedAt 等）を返す
- [ ] 404 / レート制限 / その他エラーを既存変換で返す
- [ ] `Languages` が空（フル取得と混同しない）

### C. リフレッシュ判定とメタ更新

- [ ] `refreshInterval` 未満なら再取得しない（閲覧日時のみ更新）
- [ ] `refreshInterval` 以上なら1回だけ再取得する
- [ ] `FetchedAt` ゼロ（旧データ）で再取得する
- [ ] `updated_at` 変化でスカラー項目が更新され、`Languages` が維持される
- [ ] `updated_at` 同一で `FetchedAt` のみ更新される
- [ ] リフレッシュ時に AI・README・コードを取得しない
- [ ] 自動再生成しない（解析結果を置き換えない）
- [ ] リフレッシュ失敗で Warnings を返し、キャッシュを表示する（エラーにしない）
- [ ] キャンセル時はエラー（従来どおり）
- [ ] force 時はフル取得 + 再生成で `FetchedAt` を更新する（回帰）

### D. 表示

- [ ] 詳細ヘッダに「取得: X日前 / 解析: Y日前」を表示する
- [ ] stale な解析に案内メッセージを表示する
- [ ] 一覧で stale なエントリに `◌` を表示する
- [ ] 一覧の表示で GitHub API を呼ばない
- [ ] Warnings が入力画面または詳細画面に表示される

### E. 回帰

- [ ] キャッシュヒット・ミス・force・キャンセルの既存テストが通る
- [ ] エラーメッセージが secret を含まない
- [ ] `go test ./...` / `go test -race ./...` / `go vet ./...` が成功する

## 実装順序

### Step 1: データモデルと導出

1. `RepoMeta.FetchedAt` と `Analysis.IsStale` を追加し、テスト（A）を通す。

### Step 2: GitHub client

1. `githubRepoMeta` パースを共有ヘルパーへ分離する。
2. `FetchRepositoryMeta` を実装し、テスト（B）を通す。

### Step 3: Analyzer のリフレッシュ

1. `Result` 構造体を導入し、`Analyze` を `*Result` 返しへ変更する。
2. `needsRefresh`・`mergeMeta` を実装し、テスト（C）を通す。
3. リフレッシュ失敗の警告経路を追加する。
4. TUI の `analyze` 呼び出しを `Result` 対応へ更新する。

### Step 4: 表示

1. 詳細ヘッダの日時表示と stale 案内を追加する。
2. 一覧の `◌` マークと警告表示を追加する。
3. テスト（D）を通す。

### Step 5: 統合と文書

1. 全テスト・race・vet・build を実行する。
2. SPEC 2.10・5・6.1・9.3 の実装状況を更新し、README を追記する。

## 完了条件

- キャッシュヒット時に `refreshInterval`（既定7日）を超えていれば、メタ情報が1リクエストで再取得される。
- リポジトリに更新があればスカラー項目が更新され、`Languages` は維持される。
- 古い解析は `CreatedAt < UpdatedAt` で検出され、詳細・一覧で分かる。
- AI は自動再生成されない（手動 `r` のみ）。
- リフレッシュ失敗でもキャッシュを表示し、警告を出す。
- 既存解析フローが回帰しない。
- 次のコマンドがすべて成功する。

```bash
gofmt -l .
go test ./...
go test -race ./...
go vet ./...
CGO_ENABLED=0 go build ./...
```

## 想定される変更

- `internal/core/types.go`: `RepoMeta.FetchedAt`、`Analysis.IsStale`
- `internal/core/types_test.go`: marshal / IsStale のテスト
- `internal/clients/github.go`: `FetchRepositoryMeta`、メタパース共有ヘルパー
- `internal/clients/github_test.go`: `FetchRepositoryMeta` のテスト
- `internal/analyzer/analyzer.go`: `refreshInterval`、`Result`、`needsRefresh`、`mergeMeta`
- `internal/analyzer/analyzer_test.go`: リフレッシュ・メタ更新・警告のテスト
- `internal/tui/commands.go`: `Result` 対応の委譲
- `internal/tui/commands_test.go`: fake・戻り値の追随
- `internal/tui/view.go`: 取得・解析日時、stale 案内、`◌` マーク、警告表示
- `internal/tui/view_test.go`: 表示テスト
- `SPEC.md` / `README.md`: 2.10 の実装状況と説明

## worktree

- branch: `feature-cache-freshness`
- worktree path: `/Users/issy20/ccplayground/reporepo/feature-cache-freshness`

理由: `feature-code-analysis` で抽出した `internal/analyzer` の上に鮮度管理を載せる。キャッシュヒット経路と `Result` 警告を `Analyzer` に追加するため、同パイプラインの所有者であるこのタイミングで実施する。
