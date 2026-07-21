# Plan: TUIキー操作・履歴・お気に入り管理

Status: completed

## 背景

`feature-tui-keybindings-favorites` はコミット `c818cf7` を起点としている。入力画面と詳細画面の基本キー操作、履歴選択、お気に入り更新、削除、言語・provider切替、強制再生成はすでに実装されている。

本計画では既存実装を作り直さず、キー操作の境界値と永続化失敗時の一貫性を t_wada 式 TDD で補強する。

現状には特に以下のリスクがある。

- お気に入り切替が Model の共有 Entry を非同期 command 内外から直接変更する。
- お気に入り切替を連打すると、Storeへの保存順序と画面状態が逆転し得る。
- `entriesChangedMsg` で保存エラーを設定した後、`reloadEntries` がエラーを消去する。
- 削除対象の判定がポインタ一致に依存している。
- 入力欄が空かどうかによるショートカット判定が十分にテストされていない。

## 目的

- 入力画面と詳細画面で、仕様どおりのキー操作を提供する。
- テキスト入力と単一文字ショートカットを予測可能に扱う。
- お気に入り・削除の保存成功時だけ画面状態を確定する。
- 保存失敗時に履歴、選択位置、詳細表示を変更せず、エラーを残す。
- 非同期保存中の連打や古い結果で、状態を巻き戻さない。
- 空履歴、nil Entry、範囲外選択でも panic しない。

## スコープ

### 対象

- `internal/tui/update.go`
- `internal/tui/update_test.go`
- 必要に応じて `internal/tui/model.go`
- 必要に応じてテスト用 fake Store の整理

### 対象外

- GitHub取得・AI生成・解析キャッシュの内部処理
- View、Lipgloss、Glamourの見た目
- Cobra、`main.go`、設定ウィザード
- Storeのファイル形式
- 削除確認ダイアログ

削除は現行仕様どおり `d` で即時実行する。確認ダイアログは後続UX改善とする。

## 設計方針

### 1. キー処理の優先順位

キー処理は画面状態ごとに分ける。

```text
Update
  ├─ stateInput   → updateInput
  ├─ stateLoading → updateLoading
  └─ stateDetail  → updateDetail
```

入力画面では次の優先順位とする。

1. Esc
2. Enter
3. Up / Down
4. 入力値が空の場合のみ、Tab / `l` / `p` / `f` / `d` / `q`
5. その他は textinput へ渡す

この規則により、入力途中の `p` や `l` は文字として扱う。現行仕様との互換性を保つため、入力値が空のときは予約キーをTUI操作として優先する。予約文字から始まる短縮形式はGitHub URL入力または貼り付けで回避できるが、将来的には入力フォーカスと一覧フォーカスの明示分離を検討する。

### 2. 永続化操作のメッセージ

お気に入り更新と削除を、汎用的な `entriesChangedMsg{err error}` だけで扱わない。最低限、操作IDと操作種別を持つ結果メッセージへ整理する。

例:

```go
type entryMutationKind uint8

const (
    mutationFavorite entryMutationKind = iota
    mutationDelete
)

type entryMutationFinishedMsg struct {
    requestID uint64
    kind      entryMutationKind
    fullName  string
    err       error
}
```

Modelには次を保持する。

- `mutationRequestID uint64`
- `mutationPending bool`

保存処理中の `f` / `d` は no-op とし、同じEntryへの非同期書き込みを直列化する。結果は現在の操作IDと一致するときだけ反映する。

### 3. お気に入り更新

- 対象Entryを直接変更しない。
- `cloneEntry` でコピーし、コピーの `IsFavorite` を反転する。
- commandはコピーをStoreへUpsertする。
- command goroutineからModelが参照するEntryを変更しない。
- 保存成功後にStoreを再読み込みし、entries / visible / currentを更新する。
- 保存失敗時は再読み込みせず、元のentries / visible / currentを維持してエラーを表示する。
- お気に入りタブでfavoriteを解除した場合、成功後に対象が一覧から消え、選択位置を補正する。

### 4. 履歴削除

- 削除対象はEntryポインタではなく `FullName` の大文字小文字を無視して特定する。
- Storeへ渡すsliceは新しく作り、Modelのentriesを直接変更しない。
- 保存成功後にStoreを再読み込みする。
- 保存失敗時は元のentries / visible / selected / currentを維持してエラーを表示する。
- 削除後に一覧が短くなった場合は選択位置を末尾へ補正する。
- 履歴タブ・お気に入りタブのどちらでも、表示中の対象だけを削除する。

### 5. エラー反映

- 保存エラーを先に設定してから `reloadEntries` を呼ばない。
- 成功時: 再読み込み → エラー消去。
- 失敗時: 再読み込みを行わず、ユーザー向けエラーを設定。
- Storeの生エラー、ファイルパス、秘密情報は表示しない。
- 古い操作IDの成功・失敗結果は無視する。

### 6. 言語・provider・詳細操作

- 入力画面で入力値が空なら `l` で `ja` / `en` を切り替える。
- 入力画面で入力値が空なら `p` で `claude` / `openai` を切り替える。
- 詳細画面の `l` は常に言語を切り替える。
- 切替先言語のキャッシュがあれば詳細を再描画する。
- キャッシュがなければ現在のFullNameで解析を開始する。
- 詳細画面の `r` は現在のFullNameを強制再生成する。
- 詳細画面の `f` は現在のEntryを対象にする。
- 詳細画面のEscは入力画面へ戻り、currentを解除する。
- Up / Down / PgUp / PgDnなど未予約キーはviewportへ渡す。

## TDDテストリスト

既存テストで保証済みの項目も回帰防止のため残す。未完了項目から常に一つだけ選び、redを確認してからプロダクトコードを変更する。

### A. 入力画面のナビゲーション

- [x] Downで次の履歴へ移動する
- [x] Upで前の履歴へ移動する
- [x] 先頭でUpを押しても選択位置が負にならない
- [x] 末尾でDownを押しても範囲外にならない
- [x] Tabで履歴からお気に入りへ切り替える
- [x] Tabでお気に入りから履歴へ戻る
- [x] タブ切替時に選択位置を0へ戻す
- [x] 空履歴でUp / Down / Tabを押してもpanicしない
- [x] Enterは入力値をtrimして解析へ渡す
- [x] 空入力のEnterは選択中履歴を解析する
- [x] 空入力かつ空履歴のEnterはno-opになる

### B. テキスト入力とショートカット

- [x] 入力値が空なら`l`で言語を切り替える
- [x] 入力値が空なら`p`でproviderを切り替える
- [x] 入力値が空なら`q`で終了する
- [x] Escは入力値に関係なく終了する
- [x] 入力途中の`l`を文字としてtextinputへ渡す
- [x] 入力途中の`p`を文字としてtextinputへ渡す
- [x] 入力途中の`f` / `d` / `q`を文字としてtextinputへ渡す
- [x] 入力途中のTabの期待動作をテストで固定する
- [x] 予約キー以外の通常文字をtextinputへ渡す

### C. お気に入り更新

- [x] 入力画面の`f`で選択Entryの保存commandを返す
- [x] 詳細画面の`f`でcurrentを対象にする
- [x] 空一覧の`f`はno-opになる
- [x] nil currentの詳細画面で`f`を押してもpanicしない
- [x] nil Storeでは`f`がno-opになる
- [x] favorite=falseのEntryをtrueのコピーとしてUpsertする
- [x] favorite=trueのEntryをfalseのコピーとしてUpsertする
- [x] command開始前に元Entryを変更しない
- [x] command goroutineから元Entryを変更しない
- [x] 保存成功後にentries / visible / currentを再読み込みする
- [x] お気に入りタブで解除成功後に対象がvisibleから消える
- [x] 保存失敗時に元Entryと一覧を変更しない
- [x] 保存失敗時のエラーが再読み込みで消えない
- [x] 保存エラーにStoreの生エラーを含めない
- [x] 保存中の連続`f`をno-opにする
- [x] 古いお気に入り結果を無視する

### D. 履歴削除

- [x] 入力画面の`d`で選択Entryを除いたsliceを保存する
- [x] 空一覧の`d`はno-opになる
- [x] nil Storeでは`d`がno-opになる
- [x] FullNameの大文字小文字を無視して対象を削除する
- [x] 削除用sliceの作成時にModelのentriesを変更しない
- [x] 保存成功後にentries / visibleを再読み込みする
- [x] 末尾削除後にselectedを有効範囲へ補正する
- [x] 最後の1件を削除したらselectedを0にする
- [x] お気に入りタブから対象を削除できる
- [x] 保存失敗時にentries / visible / selectedを変更しない
- [x] 保存失敗時のエラーが再読み込みで消えない
- [x] 保存エラーにStoreの生エラーを含めない
- [x] 保存中の連続`d`と`f`をno-opにする
- [x] 古い削除結果を無視する

### E. 言語・provider・詳細画面

- [x] `l`を2回押すと元の言語へ戻る
- [x] `p`を2回押すと元のproviderへ戻る
- [x] 詳細画面の`l`で切替先キャッシュを表示する
- [x] 詳細画面の`l`でキャッシュがなければ解析を開始する
- [x] nil Analysesでも詳細画面の`l`がpanicしない
- [x] 詳細画面の`r`で強制再生成する
- [x] nil currentの詳細画面で`r`はno-opになる
- [x] 詳細画面のEscで入力画面へ戻りcurrentを解除する
- [x] Up / Down / PgUp / PgDnをviewportへ渡す

### F. 非同期結果とエラー

- [x] mutation開始時に操作IDを採番してpendingにする
- [x] 現在の操作IDの成功結果だけを反映する
- [x] 現在の操作IDの失敗結果だけを反映する
- [x] 成功・失敗結果の処理後にpendingを解除する
- [x] 古い結果がentries / visible / current / errMessageを変更しない
- [x] 成功時のStore再読み込み失敗をユーザーへ表示する
- [x] 失敗時は不要なStore再読み込みを行わない

## 実装順序

### Step 1: 現状を固定する

1. `go test ./internal/tui` を実行し、既存テストがgreenであることを確認する。
2. fake Storeへ保存対象、呼び出し回数、呼び出し時点を記録する機能を最小限追加する。
3. 既存の複合テストは維持し、新規テストは振る舞い一件ごとに追加する。

### Step 2: お気に入り更新をコピーオンライト化する

最初のTDDケース:

> `f`を押してcommandを生成・実行しても、保存結果メッセージをUpdateへ反映する前は元EntryのIsFavoriteを変更しない。

1. 上記テストを追加してredを確認する。
2. `cloneEntry`で更新用Entryを作る。
3. 更新用EntryだけをUpsertする。
4. 成功結果を受けたUpdateでStoreを再読み込みする。
5. 失敗時は再読み込みせず元状態を保持する。

### Step 3: mutationの直列化と結果識別を追加する

1. Modelへmutation用IDとpending状態を追加する。
2. 結果メッセージへID、操作種別、FullNameを追加する。
3. 保存中の`f` / `d`をno-opにする。
4. 古い結果を無視する。
5. 結果処理後にpendingを解除する。

### Step 4: 削除処理を堅牢化する

1. FullNameによる削除テストを追加する。
2. Modelのentriesを変更しない保存用sliceを作る。
3. 成功時だけStoreを再読み込みする。
4. 失敗時に一覧と選択位置を保持する。
5. 削除後の選択位置を補正する。

### Step 5: 入力画面のキー境界を固定する

1. Up / Downの先頭・末尾を一件ずつテストする。
2. Tabの往復と選択位置をテストする。
3. 空入力時のショートカットを一件ずつテストする。
4. 入力途中の予約文字を一件ずつテストする。
5. q / Escの終了動作をテストする。

### Step 6: 詳細画面のキー境界を固定する

1. 言語キャッシュ有無とnil mapをテストする。
2. 強制再生成とnil currentをテストする。
3. お気に入り更新のcurrent同期をテストする。
4. Escとviewport操作をテストする。

### Step 7: リファクタリングと全体検証

1. キー判定、mutation開始、mutation結果反映の責務を小さな関数へ分ける。
2. ユーザー向けStoreエラー生成を一箇所に保つ。
3. テストhelperが本番仕様を隠さない範囲で重複を整理する。
4. テストリストをすべて完了させる。

各Stepでもテストは必ず一件ずつ red → green → refactor で進める。

## 完了条件

- TDDテストリストがすべて完了している。
- 入力画面と詳細画面の指定キー操作がテストで固定されている。
- お気に入り更新・削除がModelの共有Entryをcommand goroutineから変更しない。
- 永続化成功時だけ履歴と詳細状態を更新する。
- 永続化失敗時に元状態を維持し、ユーザー向けエラーを表示する。
- 保存中の連打と古い結果で状態が巻き戻らない。
- 空履歴、nil Store、nil current、nil Analysesでpanicしない。
- 変更したGoファイルが`gofmt`済みである。
- 以下がすべて成功する。

```bash
go test ./internal/tui
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

## 想定される変更

- `internal/tui/model.go`: mutation ID / pending状態の追加
- `internal/tui/update.go`: キー境界、コピーオンライト、非同期結果処理の補強
- `internal/tui/update_test.go`: キー操作・保存成功失敗・連打・古い結果のテスト追加
- `docs/plans/feature_tui_keybindings_favorites.md`: TDD進捗の更新

commands、view、styles、run、CLIの仕様変更は想定しない。

## 後続フェーズ

1. View・スタイル・端末リサイズの補強
2. `Run`・Cobra・`main.go`のCLI配線
3. 実ターミナルでのスモークテスト
