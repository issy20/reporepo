# Plan: フェーズ6〜7 TUI実装

Status: completed

## 目的

Bubble Tea を使った TUI を実装し、リポジトリ入力からキャッシュ確認、GitHub 情報取得、AI 解析、永続化、解析結果の閲覧までを一つの操作フローとして提供する。

本計画では `SPEC.md` の以下を実装対象とする。

- 4.3 処理フロー（解析実行時）
- 4.4 状態遷移（TUI）
- 7. キー操作仕様

`SPEC.md` 9.1 には TUI 実装済みと記載されているが、現在の `internal/tui` は `doc.go` のみであるため、実際のコードを正として新規実装する。

## スコープ

### 対象

- `internal/tui/model.go`: モデル、状態、依存関係、初期化
- `internal/tui/commands.go`: キャッシュ確認、GitHub 取得、AI 生成、保存を行う非同期コマンド
- `internal/tui/update.go`: メッセージ処理、キー操作、状態遷移
- `internal/tui/view.go`: 入力、ローディング、詳細画面の描画
- `internal/tui/styles.go`: Lipgloss のスタイル定義
- `internal/tui/run.go`: `tea.Program` の起動境界
- 上記に対応する `_test.go`

### 対象外

- `cmd/root.go`、設定ウィザード、`main.go` への配線
- GitHub / Claude / OpenAI クライアント自体の仕様変更
- Store の保存形式変更
- GoReleaser、README、配布設定

CLI から TUI を起動する配線は、TUI の公開境界が固まった後の別フェーズで行う。

## 現状と前提

- `core.Entry` はリポジトリ単位で、`Analyses` は言語（`ja` / `en`）をキーとする。
- `store.Store` は `Load`、`Save`、`Upsert` を提供する。
- `clients.GitHubClient` は `FetchRepository` を提供する。
- `clients.AIClient` は `Generate` を提供し、Claude / OpenAI の差異を隠蔽する。
- 設定の既定値は provider=`claude`、language=`ja` である。
- Bubble Tea、Bubbles、Lipgloss、Glamour は既に `go.mod` に含まれている。
- テストでは外部 API を呼ばず、fake を注入する。

## 設計方針

### 1. TUI内部の状態

画面状態は次の3つに限定する。

```text
stateInput --解析開始--> stateLoading --成功--> stateDetail
     ^                       |                    |
     |                       +--失敗/取消---------+
     +---------------------------- Esc -----------+
```

- `stateInput`: テキスト入力、履歴 / お気に入り一覧、エラー表示
- `stateLoading`: spinner と処理内容を表示
- `stateDetail`: 選択したリポジトリと解析結果を viewport で表示

モデルは少なくとも以下を保持する。

- 現在の画面状態
- `textinput.Model`、`spinner.Model`、`viewport.Model`
- 端末の幅と高さ
- 全履歴、現在表示中の一覧、選択位置、履歴 / お気に入りタブ
- 選択中の Entry
- 言語（`ja` / `en`）と provider（`claude` / `openai`）
- エラーメッセージ
- 実行中処理の cancel 関数とリクエストID
- Store、GitHub client、provider 別 AI client、現在時刻関数

### 2. 依存性の注入

TUI パッケージ側に必要最小限の Store interface を定義する。

```go
type entryStore interface {
    Load() ([]*core.Entry, error)
    Save([]*core.Entry) error
    Upsert(*core.Entry) error
}
```

GitHub は既存の `clients.GitHubClient`、AI は `clients.AIClient` を利用する。AI client は provider 名をキーとする map で注入し、テストで呼び出し回数と引数を観測可能にする。

時刻は `func() time.Time` として注入し、`ViewedAt` / `CreatedAt` のテストを決定的にする。

### 3. 非同期処理とキャンセル

解析開始時に `context.WithCancel` を生成し、`tea.Cmd` 内で次を実行する。

1. 入力を `clients.ParseRepositoryInput` で検証する。
2. Store を読み込み、対象リポジトリの選択言語キャッシュを探す。
3. キャッシュがあり強制再生成でなければ、外部 API を呼ばず `ViewedAt` を更新して保存する。
4. キャッシュがなければ GitHub からメタ情報と README を取得する。
5. 選択 provider の AI client で解析する。
6. `core.Entry` を構築して `Upsert` する。
7. 成功または失敗を独自 `tea.Msg` で返す。

Esc では context を cancel して入力画面に戻す。キャンセル後に古い command の結果が届いても画面や Store の状態を巻き戻さないよう、各処理にリクエストIDを付け、現在のIDと一致する結果だけを反映する。保存直前にも context を確認する。

### 4. キャッシュ規則

- キャッシュキーは `Entry.FullName` と選択言語とする。
- 通常解析ではキャッシュがあれば GitHub / AI を呼ばない。
- キャッシュ利用時は `ViewedAt` のみ現在時刻へ更新する。
- `r` による強制再生成ではキャッシュを無視する。
- 詳細画面で `l` を押した場合、切替先言語のキャッシュがあれば即時表示し、なければ同じリポジトリを解析する。
- provider の切替だけでは既存キャッシュを破棄しない。選択 provider で再生成したい場合は `r` を使用する。

### 5. 履歴とお気に入り

- 起動時および保存後に Store から一覧を読み直す。
- nil Entry は無視し、履歴は `ViewedAt` の降順で表示する。
- お気に入りタブは `IsFavorite == true` のみを表示する。
- `f` は選択項目または詳細項目の `IsFavorite` を反転して保存する。
- `d` は確認画面を増やさず、入力画面で選択中の項目を一覧から削除して `Save` する。
- 削除やタブ切替後は選択位置を有効範囲へ補正する。

### 6. 表示

- 入力画面には入力欄、履歴 / お気に入り、言語、provider、操作ヘルプ、直近エラーを表示する。
- ローディング画面には spinner、対象リポジトリ、処理中メッセージ、キャンセル操作を表示する。
- 詳細画面には RepoMeta と `Summary`、`TechStack`、`Background`、`Keywords` を表示する。
- 詳細本文は Glamour で Markdown 化し、viewport に設定する。
- `tea.WindowSizeMsg` で各コンポーネントの幅と高さを更新する。
- 未初期化、空一覧、nil の metadata / analysis、狭い端末でも panic しない。

Glamour のレンダリング失敗は TUI 全体の終了理由にせず、プレーンテキスト表示へフォールバックする。

### 7. キー操作

#### 入力画面

- Enter: 入力値を解析。入力が空なら選択中の履歴を開く。
- Up / Down: 一覧選択
- Tab: 履歴 / お気に入り切替
- `f`: お気に入り切替
- `d`: 選択項目削除
- `l`: `ja` / `en` 切替
- `p`: `claude` / `openai` 切替
- `q` / Esc: 終了

テキスト入力にフォーカスがあり入力値が存在する間は、通常文字を textinput に渡す。予約キーは TUI 操作を優先する。

#### ローディング画面

- Esc: 実行中処理をキャンセルして入力画面へ戻る
- spinner の tick は継続して処理する

#### 詳細画面

- Up / Down / PgUp / PgDn: viewport スクロール
- `l`: 言語切替。キャッシュがなければ解析開始
- `f`: お気に入り切替
- `r`: 選択 provider で強制再生成
- Esc: 入力画面へ戻る

## TDDテストリスト

以下の順で、常に一つだけテストを red にしてから実装する。実装中に判明したケースはこのリストへ追加する。

### A. モデル初期化と一覧

- [x] 既定設定から `stateInput`、language、provider を初期化する
- [x] Store の履歴を `ViewedAt` 降順で読み込む
- [x] nil Entry を含む履歴を安全に読み込む
- [x] Store の読み込み失敗をユーザー向けエラーとして保持する
- [x] お気に入りタブには favorite のみ表示する
- [x] 空一覧やタブ切替後に選択位置が範囲外にならない

### B. 解析コマンド

- [x] 不正なリポジトリ入力では GitHub / AI / Store 書き込みを行わない
- [x] キャッシュヒット時は GitHub と AI を呼ばない
- [x] キャッシュヒット時は `ViewedAt` を更新して保存する
- [x] キャッシュミス時は GitHub、選択 provider の AI の順に呼ぶ
- [x] AI 生成結果を新規 Entry として保存する
- [x] 既存 Entry の別言語解析を既存情報を失わず追加する
- [x] 強制再生成ではキャッシュを無視して GitHub と AI を呼ぶ
- [x] 未登録 provider は外部 API を呼ばずエラーにする
- [x] GitHub エラーを失敗メッセージへ変換する
- [x] AI エラーを失敗メッセージへ変換する
- [x] Store の Load / Upsert 失敗を失敗メッセージへ変換する
- [x] context cancel 後は保存しない

### C. 状態遷移

- [x] Enter で `stateLoading` へ遷移して解析 command を返す
- [x] 解析成功メッセージで `stateDetail` へ遷移する
- [x] 解析失敗メッセージで `stateInput` へ戻りエラーを表示する
- [x] loading 中の Esc で cancel し `stateInput` へ戻る
- [x] 古いリクエストIDの結果を無視する
- [x] 詳細画面の Esc で入力画面へ戻る

### D. 入力画面の操作

- [x] Up / Down で一覧を移動する
- [x] Tab で履歴 / お気に入りを切り替える
- [x] `l` で `ja` / `en` を切り替える
- [x] `p` で `claude` / `openai` を切り替える
- [x] `f` で選択項目のお気に入り状態を保存する
- [x] `d` で選択項目を削除して一覧を更新する
- [x] 空一覧で `f` / `d` を押しても panic しない
- [x] `q` / Esc で終了 command を返す

### E. 詳細画面の操作

- [x] Up / Down / PgUp / PgDn を viewport に渡す
- [x] `f` で詳細項目のお気に入り状態を保存する
- [x] `r` でキャッシュを無視した再解析を開始する
- [x] `l` で切替先言語のキャッシュを表示する
- [x] `l` で切替先言語のキャッシュがなければ解析を開始する

### F. 描画とリサイズ

- [x] 入力画面に入力欄、タブ、言語、provider、操作ヘルプを表示する
- [x] ローディング画面に spinner とキャンセル案内を表示する
- [x] 詳細画面にリポジトリ情報と解析4項目を表示する
- [x] エラーが入力画面に表示される
- [x] `tea.WindowSizeMsg` で textinput / viewport のサイズを更新する
- [x] 狭い端末、nil RepoMeta、nil Analysis でも View が panic しない
- [x] Glamour の失敗時にプレーンテキストへフォールバックする

### G. 起動境界

- [x] `Run` が必要な依存関係から model と `tea.Program` を構築できる
- [x] `Init` が textinput focus と spinner tick を初期化する

## 実装順序

1. テスト用 fake と、TUI が必要とする最小 interface / message 型を定義する。
2. モデル初期化と履歴一覧を TDD で実装する。
3. 画面遷移を伴わない解析 command を TDD で完成させる。
4. `stateInput` → `stateLoading` → `stateDetail` の最小縦切りを接続する。
5. キャンセルとリクエストIDによる古い結果の排除を追加する。
6. 履歴 / お気に入り / 削除 / 言語 / provider 操作を追加する。
7. 詳細画面の viewport、再生成、言語別キャッシュを追加する。
8. 各状態の View、Lipgloss、Glamour、リサイズ対応を追加する。
9. `Run` を実装し、TUI パッケージの公開境界を確定する。
10. 全体をリファクタリングし、テストリストを空にする。

各手順でもテストは一件ずつ追加し、red を確認してから green にする。

## 完了条件

- テストリストがすべて完了している。
- 解析のキャッシュヒット / ミス / 強制再生成が仕様どおり動作する。
- 3画面の状態遷移と指定キー操作が実装されている。
- テストがネットワークや利用者のホームディレクトリに依存しない。
- API key、README、AI レスポンス全文などの機密・巨大データをエラー表示へ含めない。
- 追加・変更した Go ファイルが `gofmt` 済みである。
- 以下がすべて成功する。

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

## リスクと確認事項

- `go.mod` の TUI 関連依存はすべて indirect になっているため、実装後に `go mod tidy` で direct / indirect が整理される可能性がある。
- Bubble Tea / Bubbles の固定バージョンに合わせ、実際の API をコンパイルで確認しながら進める。
- `Analyses` は言語のみをキーとするため、provider 切替だけでは同一言語の別 provider 結果を並存できない。本フェーズでは既存データモデルを維持し、`r` で上書き再生成する。
- Store は読み込み後の更新をトランザクションとして提供しない。同時操作は Bubble Tea の単一 Update ループで直列化し、解析 command の多重実行はリクエストIDで防ぐ。
- `d` が即時削除でよいか、将来確認ダイアログが必要かは UX 改善事項として後続へ回す。

## 後続フェーズ

- Cobra の `run` コマンドと `main.go` から TUI を起動する配線
- `reporepo config` の設定ウィザード
- 実ターミナルでの手動スモークテスト
- README、GoReleaser、リリース設定
