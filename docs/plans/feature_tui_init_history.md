# Plan: TUIモデル初期化と履歴読み込み

Status: completed

## 背景

`feature-tui-init` はコミット `09a2c99`（`feat: implement Bubble Tea TUI`）を起点としており、対象となる `NewModel`、`reloadEntries`、`refreshVisible` と基本テストはすでに存在する。

したがって本計画では既存実装を削除して作り直さず、現在の振る舞いをテストで固定したうえで、不足している境界値と復旧処理を t_wada 式 TDD で補強する。

## 目的

TUI 起動時に、設定・依存コンポーネント・履歴一覧が決定的かつ安全に初期化されることを保証する。

- 初期画面を `stateInput` にする。
- 設定値から言語と AI provider を決定し、不正値には安全な既定値を使う。
- Bubble Tea の入力、spinner、viewport を利用可能な状態にする。
- Store の履歴から nil を除外し、`ViewedAt` の新しい順に表示する。
- お気に入り表示と選択位置を一貫した状態に保つ。
- Store が nil、空、読み込み失敗、失敗後の復旧というケースでも panic しない。
- Store から受け取ったスライスや Entry の並びを初期化処理が意図せず変更しない。

## スコープ

### 対象

- `internal/tui/model.go`
- `internal/tui/model_test.go`
- 必要な場合のみ、テスト共通 fake の整理

### 対象外

- GitHub 取得、AI 生成、キャッシュ更新
- Bubble Tea のキー操作と画面遷移
- View、Markdown、viewport の本文描画
- お気に入り更新、履歴削除
- `Run`、Cobra、`main.go` の配線
- Store 本体の永続化仕様変更

## 既存の設計境界

### コンストラクタ

```go
func NewModel(deps Dependencies, cfg *core.Config) Model
```

`NewModel` は次を担当する。

1. 言語と provider の検証・既定値設定
2. `textinput.Model`、`spinner.Model`、`viewport.Model` の生成
3. `Dependencies` の保持と nil 依存の既定値設定
4. Store からの初回履歴読み込み
5. 履歴タブ向け表示一覧の生成

外部 API 呼び出しや Store 書き込みは行わない。

### 依存関係

```go
type Dependencies struct {
    Store    entryStore
    GitHub   clients.GitHubClient
    AI       map[string]clients.AIClient
    Now      func() time.Time
    Renderer markdownRenderer
}
```

- `Now == nil` の場合は `time.Now` を使用する。
- `Renderer == nil` の場合は `glamourRenderer` を使用する。
- Store、GitHub、AI は未設定でもコンストラクタで panic させない。実際に必要となる操作でユーザー向けエラーにする責務は後続処理が持つ。

## 仕様

### 1. 基本状態

- `state = stateInput`
- `tab = tabHistory`
- `selected = 0`
- `current = nil`
- `errMessage` は履歴読み込みに成功した場合は空
- text input はフォーカス済み
- placeholder はリポジトリ入力形式を説明する
- spinner は Dot を使用する
- viewport はゼロサイズで生成し、後続の `tea.WindowSizeMsg` で更新する

### 2. 言語とprovider

| Config | language | provider |
|---|---|---|
| `nil` | `ja` | `claude` |
| 空値 | `ja` | `claude` |
| `ja` / `claude` | `ja` | `claude` |
| `en` / `openai` | `en` | `openai` |
| 未対応言語 | `ja` | 有効なら指定provider |
| 未対応provider | 有効なら指定言語 | `claude` |

provider は現在利用可能な `claude` と `openai` のみを受理する。前後の空白を暗黙に有効化せず、不正な設定として既定値へ戻す。

### 3. 履歴読み込み

- Store が nil の場合は空履歴とし、エラーにしない。
- `Load` が空または nil slice を返した場合も空履歴とする。
- nil Entry を除外する。
- `ViewedAt` の降順で安定ソートする。
- 同じ `ViewedAt` の Entry は Store が返した順序を維持する。
- Store が所有するスライスの要素順序を変更しない。
- Entry 自体は同一ポインタを参照し、不要な deep copy は行わない。
- 読み込み失敗時は秘密情報や生エラーを表示せず、ユーザー向けメッセージを `errMessage` に保持する。
- 再読み込みに成功したら、過去の履歴読み込みエラーを消去する。

### 4. 表示一覧と選択位置

- 履歴タブでは nil 除外後の全 Entry を表示する。
- お気に入りタブでは `IsFavorite == true` の Entry のみ表示する。
- 表示対象が空なら `selected = 0` とする。
- `selected < 0` なら `0` に補正する。
- `selected >= len(visible)` なら末尾に補正する。
- 絞り込みや再読み込み後にも範囲外インデックスを残さない。

## TDDテストリスト

既存テストで保証済みの項目も、回帰防止のためリストに残す。新規作業では未完了項目から一つだけ選び、必ず red を確認してから product code を変更する。

### A. 初期状態と設定

- [x] 空 Config で `stateInput`、`ja`、`claude` を使用する
- [x] nil Config で `stateInput`、`ja`、`claude` を使用する
- [x] `en` と `openai` を指定した場合に設定値を使用する
- [x] 未対応言語を `ja` にフォールバックする
- [x] 未対応providerを `claude` にフォールバックする
- [x] text input がフォーカス済みで placeholder と文字数上限を持つ
- [x] spinner が Dot で初期化される
- [x] viewport、タブ、選択位置、current、エラーが初期値になる

### B. 依存関係

- [x] 注入した Store、GitHub、AI map、Now、Renderer を保持する
- [x] `Now == nil` でも呼び出し可能な時刻関数を持つ
- [x] `Renderer == nil` なら既定 renderer を持つ
- [x] Store が nil でも panic せず空履歴になる

### C. 履歴読み込み

- [x] 履歴を `ViewedAt` の降順で読み込む
- [x] nil Entry を除外する
- [x] 読み込み失敗をユーザー向けエラーとして保持する
- [x] nil slice と空 slice を空履歴として扱う
- [x] 同じ `ViewedAt` の Entry の順番を維持する
- [x] Store が返した元スライスの順番と要素を変更しない
- [x] 読み込み失敗後の再読み込み成功でエラーを消去する
- [x] 再読み込み後の履歴と表示一覧を最新データへ置き換える

### D. 表示一覧と選択位置

- [x] お気に入りタブでは favorite のみ表示する
- [x] 大きすぎる選択位置を末尾へ補正する
- [x] 履歴タブでは全履歴を表示する
- [x] お気に入りが0件なら選択位置を0にする
- [x] 負の選択位置を0に補正する
- [x] 再読み込みで件数が減っても選択位置が範囲内になる

### E. Init command

- [x] `Init` が text input blink と spinner tick の両方を開始する command を返す

## 実装手順

### Step 1: 現状を固定する

1. `go test ./internal/tui` を実行し、既存テストが green であることを確認する。
2. 既存の `fakeStore` を、ロード結果の差し替えと呼び出し回数の確認ができる範囲だけ拡張する。
3. テストリストの未完了項目から一つだけ選ぶ。

### Step 2: 設定値の検証を完成させる

1. nil Config のテストを追加して実行する。
2. 有効な `en` / `openai` のテストを追加する。
3. 未対応言語のテストを追加する。
4. 未対応providerのテストを追加し、現在の任意文字列を受理する実装に対して red を確認する。
5. 言語とproviderの判定を小さな関数へ分離するか、`NewModel` 内で最小修正する。

各項目を同時に追加せず、一件ごとに red → green → refactor を行う。

### Step 3: UI部品と依存関係を固定する

1. text input、spinner、viewport の初期値を一項目ずつテストする。
2. 注入された依存が保持されることをテストする。
3. nil Now / Renderer / Store の振る舞いをテストする。

### Step 4: 履歴読み込みを堅牢化する

1. nil / 空結果をテストする。
2. 同一時刻の安定順序をテストする。
3. Store が返したスライスを複製してから nil 除外・ソートするようにし、元スライス非破壊テストを green にする。
4. 読み込み失敗後に Store を成功状態へ切り替えて `reloadEntries` を呼び、エラーが消えることをテストする。
5. 再読み込み時に entries / visible が置換され、selected が補正されることをテストする。

### Step 5: 表示一覧とInitを完成させる

1. 履歴・お気に入り双方のフィルタをテストする。
2. 空・負・過大な選択位置を一件ずつテストする。
3. `Init` が初期 command を返すことを、副作用を長時間待たずに検証する。

### Step 6: リファクタリングと全体検証

1. テスト名を振る舞い単位にそろえる。
2. テストデータ生成が重複する場合のみ helper 化する。
3. `NewModel` が肥大化する場合は、設定正規化と履歴整形を副作用のない関数へ抽出する。
4. テストリストがすべて完了したことを確認する。

## 実装上の注意

- テストのためだけに本番APIを過度に公開しない。同一 `tui` package のテストから非公開状態を検証する。
- `sort.SliceStable` を Store の返却 slice に直接適用しない。新しい slice に有効な Entry を集めてからソートする。
- Store の具体型へ依存せず、既存 `entryStore` interface を維持する。
- 初期化でファイル書き込み、ネットワークアクセス、AI呼び出しを行わない。
- ユーザー向けエラーに Store のパスや生エラー全文を含めない。
- Bubble Tea の component API は現在の依存バージョンでコンパイルして確認する。
- 既存の解析、更新、描画テストを壊さない。

## 完了条件

- TDDテストリストがすべて完了している。
- `NewModel` が有効設定、不正設定、nil依存を安全に処理する。
- 履歴が nil 除外済みかつ `ViewedAt` 降順である。
- Store の返却 slice を変更しない。
- 読み込み失敗後に復旧でき、古いエラーを残さない。
- 表示一覧と選択位置が常に整合している。
- プロダクトコードとテストが `gofmt` 済みである。
- 以下がすべて成功する。

```bash
go test ./internal/tui
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

## 想定される変更

- `internal/tui/model.go`: 設定正規化、履歴コピー、エラー復旧の小規模修正
- `internal/tui/model_test.go`: 初期化・履歴境界値のテスト追加
- `docs/plans/feature_tui_init_history.md`: TDD進捗のチェック更新

commands、update、view の仕様変更は想定しない。
