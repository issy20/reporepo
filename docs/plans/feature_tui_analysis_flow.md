# Plan: TUI解析フローと非同期状態遷移

Status: completed (既存実装の仕様固定・TDD補強完了)

## 背景

`feature-tui-analysis-flow` はコミット `73fb153` を起点としている。TUI の解析コマンドと状態遷移には、すでに以下の基本実装が存在する。

- 入力の解析
- 言語別キャッシュの利用
- GitHub 取得と AI 生成
- Store への保存
- `stateInput` / `stateLoading` / `stateDetail` の遷移
- context によるキャンセル
- リクエストIDによる古い結果の排除

本計画では既存実装を作り直さず、「解析コマンドとキャッシュ判定」と「状態遷移と非同期結果の反映」を一つの縦切り機能として監査し、不足している境界値・失敗時の一貫性・競合ケースを t_wada 式 TDD で補強する。

## 目的

次の解析フローを、成功・失敗・キャンセルのすべてで一貫した状態にする。

```text
stateInput
  └─ Enter
      └─ stateLoading
          ├─ 入力検証
          ├─ キャッシュヒット → ViewedAt更新 → 保存
          └─ キャッシュミス
              ├─ GitHub取得
              ├─ 選択providerでAI生成
              └─ Entry保存
                  ├─ 成功 → stateDetail
                  ├─ 失敗 → stateInput + 安全なエラー
                  └─ Esc  → cancel + stateInput
```

特に次を保証する。

- キャッシュヒットでは GitHub / AI を呼ばない。
- キャッシュミスでは GitHub → AI → Store の順で処理する。
- 保存失敗やキャンセル時に、Model が参照する既存 Entry を途中状態へ変更しない。
- API key、ファイルパス、外部サービスの生エラーをユーザー表示へ含めない。
- 古い非同期結果が現在の画面・履歴・エラーを上書きしない。
- nil や不正な非同期メッセージでも panic しない。

## スコープ

### 対象

- `internal/tui/commands.go`
- `internal/tui/commands_test.go`
- `internal/tui/update.go`
- `internal/tui/update_test.go`
- 必要な場合のみ `internal/tui/model.go` のリクエスト管理フィールド
- テスト fake / helper の整理

### 対象外

- View、Lipgloss、Glamour の見た目
- 履歴・お気に入りの一般操作
- `Run`、Cobra、`main.go` の配線
- GitHub / Claude / OpenAI クライアント内部のHTTP仕様
- Store の保存形式
- `core.Entry.Analyses` のキー構造変更

`Analyses` は現在どおり言語をキーとする。provider を切り替えただけでは同一言語のキャッシュを無効化せず、選択providerで上書きしたい場合は強制再生成を使用する。

## 責務分割

### commands.go

外部依存を伴う解析処理だけを担当する。

1. 入力検証
2. Store 読み込み
3. キャッシュ判定
4. GitHub 取得
5. AI client 選択・生成
6. Entry のコピーオンライト更新
7. context の確認
8. Store 保存
9. 成功 / 失敗メッセージ生成

commands は画面状態を直接変更しない。

### update.go

Bubble Tea の状態遷移だけを担当する。

1. 解析開始と `stateLoading` への遷移
2. cancel 関数とリクエストIDの管理
3. 成功結果の `stateDetail` への反映
4. 失敗結果の `stateInput` への反映
5. Esc キャンセル
6. 古いリクエスト結果の破棄
7. spinner の継続

Update は GitHub / AI / Store の同期処理を直接実行しない。

## メッセージ設計

既存のメッセージ構造を維持する。

```go
type analysisSucceededMsg struct {
    requestID uint64
    entry     *core.Entry
}

type analysisFailedMsg struct {
    requestID uint64
    err       error
}
```

- command 開始時に採番した `requestID` を成功・失敗の両方へ必ず引き継ぐ。
- Update は `msg.requestID == m.requestID` の場合だけ結果を反映する。
- 成功メッセージの Entry が nil、失敗メッセージの error が nil の場合は panic せず、安全な失敗として入力画面へ戻す。
- Esc では context を cancel し、リクエストIDを進めて実行中結果を無効化する。

## 解析コマンド仕様

### 1. 入力検証

- `clients.ParseRepositoryInput` を利用する。
- 不正入力時は Store Load、GitHub、AI、Store書き込みを行わない。
- ユーザーには入力形式が不正であることだけを返す。

### 2. キャッシュヒット

- `FullName` は大文字小文字を区別せず検索する。
- 選択言語の Analysis があり、`force == false` ならキャッシュヒットとする。
- GitHub と AI は呼ばない。
- `ViewedAt` を注入した `Now` で更新して Upsert する。
- Upsert に成功した場合のみ更新済み Entry を返す。
- Upsert に失敗またはキャンセルした場合、読み込み元 Entry の `ViewedAt` を変更しない。

### 3. キャッシュミス・強制再生成

- 選択providerに対応する AI client がなければ GitHub を呼ぶ前に失敗する。
- GitHub client がなければ失敗する。
- GitHub から `RepositoryData` を取得する。
- nil data / nil metadata を拒否する。
- 選択providerの AI clientへ metadata、README、language を渡す。
- nil Analysis を拒否する。
- context cancel を確認してから保存する。
- 既存 Entry を更新する場合はコピーオンライトとし、保存成功前に元 Entry、Analyses map、RepoMeta を変更しない。
- 既存の他言語Analysis、favorite、CreatedAtを保持する。
- 新規 Entry は初回保存時に CreatedAt と ViewedAt を同じ時刻にする。
- GitHub metadata の FullName が空なら入力から作った `owner/repo` を使用する。
- `force == true` なら同一言語キャッシュを無視して再生成する。

### 4. エラー

- Store Load / Upsert、GitHub、AI の生エラーを利用者向けエラーへ変換する。
- 生エラー文字列、API key、token、ローカルファイルパスを返却エラーへ含めない。
- `context.Canceled` 相当は「解析をキャンセルしました」として扱う。
- エラー発生時は後続依存を呼ばない。

## 状態遷移仕様

### 1. 解析開始

- 入力画面の Enter で入力値を trim して解析開始する。
- 入力が空なら選択中履歴の FullName を使用する。
- 入力も選択履歴もなければ何もしない。
- `context.WithCancel` を作り、cancel 関数を Model に保持する。
- リクエストIDを一つ進める。
- state を `stateLoading` にする。
- 以前のエラーを消去する。
- 対象リポジトリを loading label に設定する。
- 解析 `tea.Cmd` を返す。

### 2. 成功

- 現在のリクエストIDと一致する結果だけ処理する。
- cancel 関数を破棄する。
- Entry を current に設定する。
- state を `stateDetail` にする。
- エラーを消去する。
- 履歴を再読み込みする。
- 詳細コンテンツを再構築する。
- nil Entry は安全な失敗として処理する。

### 3. 失敗

- 現在のリクエストIDと一致する結果だけ処理する。
- cancel 関数を破棄する。
- state を `stateInput` に戻す。
- 安全なユーザー向けエラーを保持する。
- nil error でも panic しない。
- current と正常な履歴を不用意に変更しない。

### 4. キャンセル・古い結果

- loading 中の Esc で cancel を呼ぶ。
- cancel 関数を nil にする。
- リクエストIDを進め、実行中結果を無効化する。
- state を `stateInput` に戻す。
- キャンセル後に届いた成功・失敗メッセージを無視する。
- 古い結果を無視した際、state、current、entries、errMessageを変更しない。
- loading 中は Enter その他のキーで二重解析を開始しない。

## TDDテストリスト

既存テストで保証済みの項目も回帰防止のため残す。未完了項目から常に一つだけ選び、red を確認してから product code を変更する。

### A. 入力とキャッシュ

- [x] 不正入力では GitHub / AI / Upsert を呼ばない
- [x] 不正入力では Store Load も呼ばない
- [x] キャッシュヒットでは GitHub / AI を呼ばない
- [x] キャッシュヒットで `ViewedAt` を更新して Upsert する
- [x] FullName の大文字小文字が異なってもキャッシュヒットする
- [x] 対象言語の Analyses map が nil でもpanicせずキャッシュミスになる
- [x] キャッシュ保存失敗時に元 Entry の `ViewedAt` を変更しない
- [x] キャッシュ処理開始前にcancel済みなら Upsertしない

### B. GitHub・AI生成・保存

- [x] キャッシュミスで GitHub と AI を各1回呼ぶ
- [x] GitHub → AI → Store の呼び出し順を保証する
- [x] GitHubへ正しいowner / repoを渡す
- [x] AIへmetadata / README / languageをそのまま渡す
- [x] 既存の他言語Analysis、favorite、CreatedAtを保持する
- [x] 強制再生成でキャッシュを無視する
- [x] 未登録providerでは GitHub と Upsert を呼ばない
- [x] nil AI mapとnil AI clientを安全に拒否する
- [x] nil GitHub clientを安全に拒否する
- [x] nil RepositoryDataを拒否しAIを呼ばない
- [x] nil RepoMetaを拒否しAIを呼ばない
- [x] nil Analysisを拒否しUpsertしない
- [x] 新規EntryのCreatedAt / ViewedAt / FullName / Analysesを正しく設定する
- [x] 空のGitHub FullNameを入力のowner/repoで補完する
- [x] 保存失敗時に既存EntryとAnalyses mapを変更しない
- [x] 保存成功時だけ生成結果を返す

### C. キャンセルとエラー安全性

- [x] AI処理中のcancel後にUpsertしない
- [x] 開始前にcancel済みならGitHubを呼ばない
- [x] GitHub完了後のcancelではAIを呼ばない
- [x] AI完了後のcancelではUpsertしない
- [x] Load / GitHub / AI / Upsertエラーを失敗として返す
- [x] Loadエラー後にGitHub / AI / Upsertを呼ばない
- [x] GitHubエラー後にAI / Upsertを呼ばない
- [x] AIエラー後にUpsertを呼ばない
- [x] 変換後エラーに依存元の秘密文字列を含めない
- [x] 成功・失敗メッセージにrequestIDを引き継ぐ

### D. 解析開始

- [x] Enterで`stateLoading`へ遷移してcommandを返す
- [x] 解析開始時にrequestIDを採番する
- [x] 解析開始時にcancel関数を保持する
- [x] 解析開始時に以前のエラーを消去する
- [x] loading labelに対象リポジトリを設定する
- [x] 空入力では選択中履歴のFullNameを使用する
- [x] 空入力かつ空履歴では解析を開始しない
- [x] loading中のEnterでは二重解析を開始しない

### E. 成功・失敗結果

- [x] 成功結果で`stateDetail`へ遷移してcurrentを設定する
- [x] 成功結果でcancelとエラーを消去する
- [x] 成功結果で履歴を再読み込みする
- [x] 成功結果で詳細コンテンツを更新する
- [x] nil Entryの成功メッセージでもpanicしない
- [x] 失敗結果で`stateInput`へ戻りエラーを設定する
- [x] 失敗結果でcancelを消去する
- [x] nil errorの失敗メッセージでもpanicしない
- [x] 失敗結果で既存currentと履歴を変更しない

### F. キャンセルと古い結果の排除

- [x] loading中のEscでcancelしrequestIDを無効化する
- [x] loading中のEscで`stateInput`へ戻りエラーを消す
- [x] 古い成功メッセージを無視する
- [x] 古い失敗メッセージを無視する
- [x] キャンセル後の成功メッセージがstate / current / entriesを変更しない
- [x] キャンセル後の失敗メッセージがerrMessageを変更しない
- [x] 現在のrequestIDだけが一度反映される
- [x] loading中もspinner tickを継続する

## 実装順序

### Step 1: 現状を固定する

1. `go test ./internal/tui` を実行し、既存テストがgreenであることを確認する。
2. fake Store / GitHub / AIに、呼び出し引数・順序・cancel hookを記録する最小機能を追加する。
3. 既存テストを変更せず、新しいテストは一件ずつ追加する。

### Step 2: キャッシュ経路を完成させる

1. 不正入力でLoadも呼ばれないことをテストする。
2. 大文字小文字を無視したキャッシュ検索を固定する。
3. nil Analyses mapをテストする。
4. Upsert失敗時に元Entryを変更しないテストをredにする。
5. EntryをコピーしてからViewedAtを更新する最小実装でgreenにする。

### Step 3: キャッシュミス経路を完成させる

1. GitHub / AIの引数と呼び出し順を一項目ずつテストする。
2. nil依存・nil応答を一項目ずつテストする。
3. 新規Entryの全フィールドをテストする。
4. 保存失敗時の元Entry非破壊テストをredにする。
5. EntryとAnalyses mapをコピーオンライトで更新する。

### Step 4: エラーとキャンセルを完成させる

1. 各依存エラー後に後続処理を呼ばないことをテストする。
2. 秘密文字列を含まないことをテストする。
3. 開始前、GitHub後、AI後のcancelを一件ずつテストする。
4. 成功・失敗メッセージのrequestID引き継ぎをテストする。

### Step 5: 状態遷移へ接続する

1. 解析開始時のcancel、エラー消去、labelを一項目ずつ固定する。
2. 空入力から選択履歴を解析するケースをテストする。
3. 空入力・空履歴とloading中Enterがno-opであることをテストする。
4. 成功結果の履歴再読込と詳細更新をテストする。
5. 失敗結果の状態保持をテストする。

### Step 6: 非同期競合を排除する

1. 古い失敗結果を無視するテストを追加する。
2. Esc後に成功結果が届くテストを追加する。
3. Esc後に失敗結果が届くテストを追加する。
4. 同じrequestIDの結果が二度届く場合の期待動作をテストで固定する。
5. spinner tickの継続をテストする。

### Step 7: リファクタリングと検証

1. コピーオンライト処理を小さなhelperへ抽出する。
2. エラー変換を一箇所にまとめる。
3. テストfakeの責務と命名を整理する。
4. テストリストをすべて完了させる。

各Step内でもテストは必ず一件ずつ red → green → refactor で進める。二つの機能を同じworktreeで扱うが、複数テストをまとめてredにしない。

## 最初のTDDサイクル

最初に次の一件を実装する。

> キャッシュヒット後のUpsertが失敗した場合、Storeから読み込んだ元EntryのViewedAtを変更しない。

現在の実装は既存Entryを直接変更してからUpsertするため、このテストはredになる見込みである。Entryをコピーして更新し、保存成功時だけ更新済みEntryを返すことでgreenにする。

その後、状態遷移側の最初の追加ケースとして次を実装する。

> キャンセル後に同じ解析処理の成功メッセージが届いても、入力画面・current・履歴を変更しない。

## 完了条件

- TDDテストリストがすべて完了している。
- キャッシュヒット / ミス / 強制再生成が仕様どおり動作する。
- 保存失敗とキャンセルで既存Entryを途中変更しない。
- エラーに依存元の秘密情報を含めない。
- 成功・失敗・キャンセルで画面状態とcancel関数が整合する。
- 古い非同期結果が現在のModelを変更しない。
- nilメッセージ・nil依存・nil応答でpanicしない。
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

- `internal/tui/commands.go`: コピーオンライト、cancel確認、エラー変換の補強
- `internal/tui/commands_test.go`: キャッシュ・引数・順序・失敗時一貫性のテスト追加
- `internal/tui/update.go`: nilメッセージ、結果の一度限り反映、競合処理の補強
- `internal/tui/update_test.go`: 成功・失敗・キャンセル競合のテスト追加
- `docs/plans/feature_tui_analysis_flow.md`: TDD進捗の更新

View、styles、run、CLIの変更は想定しない。
