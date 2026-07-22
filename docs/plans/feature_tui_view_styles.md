# Plan: TUI View・スタイル・端末リサイズ

Status: complete (TDD実装・回帰テスト完了)

## 背景

`feature-tui-view-styles` はコミット `17a7bf9` を起点としている。入力・ローディング・詳細画面、Lipglossスタイル、GlamourによるMarkdown描画、viewport、`tea.WindowSizeMsg` の基本実装はすでに存在する。

本計画では既存の表示構成を維持しながら、端末サイズ、長文、nilデータ、レンダリング失敗、スクロール位置、外部入力に含まれる制御文字を t_wada 式 TDD で補強する。

現状には主に次の課題がある。

- 入力画面が履歴を全件描画するため、端末の高さを超える。
- 長いリポジトリ名、説明、エラー、ヘルプが端末幅を超え得る。
- リサイズ時の `setDetailContent` が必ず `GotoTop` し、閲覧位置を失う。
- `renderer == nil` のゼロ値Modelで詳細再描画するとpanicする可能性がある。
- GitHub / AI由来テキストの制御文字がそのまま端末へ渡り得る。
- テストが複数画面の存在確認にまとまっており、境界値ごとの失敗原因が分かりにくい。

## 目的

- 3画面を端末サイズに応じて安全に描画する。
- 幅・高さが0または極端に小さくてもpanicしない。
- 履歴一覧を利用可能な高さに収め、選択行を常に表示する。
- 長い表示内容を折り返しまたは省略し、横方向の崩れを抑える。
- 詳細画面のMarkdownを端末幅に合わせて再描画する。
- リサイズ時は詳細画面の閲覧位置を可能な限り維持する。
- EntryやRendererがnilでも安全なフォールバックを表示する。
- 外部由来テキストから危険な端末制御文字を除去する。
- 見た目のANSIコードそのものではなく、意味とレイアウトを安定したテストで保証する。

## スコープ

### 対象

- `internal/tui/view.go`
- `internal/tui/view_test.go`
- `internal/tui/styles.go`
- `internal/tui/update.go` の `tea.WindowSizeMsg` 処理
- 必要に応じて `internal/tui/model.go` の表示用状態

### 対象外

- キー割り当て、履歴更新、お気に入り保存
- GitHub取得、AI生成、解析キャッシュ
- Cobra、`main.go`、設定ウィザード
- テーマ選択UI
- マウス操作
- Storeの保存形式

## 画面構成

### 1. 入力画面

```text
reporepo

> owner/repo または GitHub URL

[履歴]  お気に入り
> owner/repo ★
  another/repo
  ...

言語: ja  provider: claude
Enter: 開く ... q/Esc: 終了

エラー（存在する場合）
```

- タイトル、入力欄、タブ、状態行、ヘルプ、エラーの固定領域を先に確保する。
- 残った高さを履歴表示行数として使用する。
- 選択中Entryが必ず表示範囲へ入るよう、一覧の開始位置を計算する。
- 一覧が空の場合は「項目はありません」を1行表示する。
- 長いFullNameは表示幅に合わせて省略する。元データは変更しない。
- favoriteは `★` で示し、選択行はカーソルと選択スタイルで示す。
- エラーがある場合も固定領域と一覧の高さ計算が破綻しないようにする。

### 2. ローディング画面

```text
⠋ 解析しています: owner/repo

Esc: キャンセル
```

- spinner、loading label、キャンセル案内を表示する。
- 空labelでは既定文言を表示する。
- 長いlabelは端末幅へ折り返しまたは省略する。
- 小さい端末でも空文字やpanicにならない。

### 3. 詳細画面

- viewport本文と1行の操作ヘルプを表示する。
- viewportの幅は安全な内側幅、縦幅はヘルプ領域を除いた値にする。
- currentがnilなら安全な空状態を表示する。
- RepoMetaがnilでもリポジトリ名と解析結果は表示する。
- 選択言語のAnalysisがnilなら「解析結果がありません」を表示する。
- Markdownには以下を含める。
  - リポジトリ名
  - 説明、Stars、Forks、主要言語
  - Summary
  - Tech Stack
  - Background
  - Keywords
- Glamour失敗時は同じ情報をプレーンテキストで表示する。

## レイアウト設計

### 1. サイズ正規化

`tea.WindowSizeMsg` の値は0や負数相当の計算結果を前提に扱う。

```go
safeWidth  := max(1, msg.Width)
safeHeight := max(1, msg.Height)
```

各componentへ設定する値も必ず1以上とする。マージンやヘルプ行を引く処理は小さなレイアウト関数へ集約し、ViewとUpdateで計算式を重複させない。

例:

```go
type layout struct {
    width          int
    inputWidth     int
    viewportWidth  int
    viewportHeight int
    historyHeight  int
}
```

### 2. 履歴ウィンドウ

Modelの `visible` は全表示候補を保持したままにする。View側で次を計算する。

- 表示可能行数
- 開始index
- 終了index

`selected` が範囲内であることを前提にせず、防御的に補正した値を描画へ使う。ViewはModelを変更しない。

### 3. 横幅制御

- ANSI幅を考慮できるLipgloss / Charmbraceletの幅・省略機能を優先する。
- FullName、状態行、ヘルプ、エラーは安全な幅へ収める。
- Markdown本文はGlamourのword wrapへ委ねる。
- Unicode、全角文字、絵文字をbyte長で切断しない。
- 幅が極端に小さい場合は完全な見た目より非panicと最低限の情報を優先する。

### 4. 詳細再描画とスクロール位置

用途を分ける。

- Entry変更・言語変更: 新しい内容として先頭へ移動する。
- 端末リサイズ: Markdownを再wrapし、以前のスクロール比率を可能な範囲で維持する。

`setDetailContent` に「先頭へ戻す」「位置を維持する」の意図を渡すか、別メソッドに分割する。高さや本文が短くなった場合はviewportの有効範囲へclampする。

### 5. 外部テキストの安全性

RepoMetaおよびAnalysis由来の文字列から、改行とタブ以外のC0制御文字、ESC、DELを除去してからMarkdownを組み立てる。保存データ自体は変更せず、表示境界でのみ無害化する。

対象:

- FullName
- Description
- Language
- Summary
- TechStack
- Background
- Keywords
- loading label
- errMessage

## スタイル方針

- `titleStyle`: アプリ名
- `activeStyle`: 選択中タブ
- `selectedStyle`: 選択中の履歴行（追加予定）
- `favoriteStyle`: favorite記号（必要なら追加）
- `dimStyle`: 操作ヘルプ、補助情報
- `errorStyle`: エラー

スタイルの色番号やANSIエスケープ列を直接テストしない。色が無効な端末でも、カーソル、記号、テキストによって状態を識別可能にする。

## TDDテストリスト

既存テストで保証済みの項目も回帰防止のため残す。未完了項目から一つだけ選び、redを確認してからプロダクトコードを変更する。

### A. サイズとレイアウト

- [x] 通常サイズのWindowSizeMsgでinput / viewportを更新する
- [x] 幅0・高さ0でもcomponentサイズが1以上になる
- [x] 幅1・高さ1でもViewがpanicしない
- [x] 負の余白計算がcomponentへ渡らない
- [x] 通常サイズでviewportがヘルプ領域を除いた高さになる
- [x] 入力画面の履歴表示可能行数を端末高さから算出する
- [x] WindowSizeMsgを連続で受けてもサイズが整合する
- [x] 未知のscreenStateでも入力画面として安全に描画する

### B. 入力画面

- [x] タイトル、入力欄、履歴、お気に入り、言語、provider、ヘルプを表示する
- [x] 履歴タブを選択状態として表示する
- [x] お気に入りタブを選択状態として表示する
- [x] 選択中Entryにカーソルを表示する
- [x] favorite Entryに`★`を表示する
- [x] 空一覧に空状態を表示する
- [x] 選択位置が範囲外でもpanicしない
- [x] 履歴件数が表示可能行数を超えても高さ内に収める
- [x] 選択位置を移動すると表示ウィンドウも追従する
- [x] 長いFullNameをUnicode単位で安全に省略する
- [x] エラーを表示する
- [x] 長いエラーを端末幅へ収める
- [x] nil Entryをvisibleに含めてもpanicしない
- [x] View呼び出しがModelのentries / visible / selectedを変更しない

### C. ローディング画面

- [x] loading labelとキャンセル案内を表示する
- [x] 空labelで既定文言を表示する
- [x] spinnerの現在フレームを表示する
- [x] 長いlabelを端末幅へ収める
- [x] 幅1・高さ1でも非空文字列を返す
- [x] label内の制御文字を除去する

### D. 詳細Markdown

- [x] FullName、Summary、Tech Stack、Background、Keywordsを含める
- [x] Description、Stars、Forks、Languageを含める
- [x] nil RepoMetaでもpanicしない
- [x] nil Analysesでもpanicしない
- [x] nil Entryで空状態を返す
- [x] 空Keywordsを安全に表示する
- [x] 複数Keywordsを区切って表示する
- [x] Unicodeと長文を欠損なくrendererへ渡す
- [x] 外部文字列のESC・制御文字を除去する
- [x] Markdown記号を含むFullNameや説明の扱いをテストで固定する

### E. Rendererとフォールバック

- [x] renderer失敗時にプレーンテキストへフォールバックする
- [x] rendererへ安全な幅を渡す
- [x] 端末リサイズ後は変更後の幅で再Renderする
- [x] nil rendererでは既定rendererまたはプレーンテキストを使用する
- [x] rendererが空文字を成功として返してもViewが安全に動作する
- [x] rendererエラーの生メッセージを画面へ表示しない

### F. Viewportとスクロール

- [x] 詳細本文がviewportへ設定される
- [x] Entry変更時はviewportを先頭へ戻す
- [x] 言語変更時はviewportを先頭へ戻す
- [x] 端末リサイズ時はスクロール比率を維持する
- [x] リサイズ後に短くなった本文ではoffsetを有効範囲へ補正する
- [x] currentがnilなら安全な空状態と戻る案内を表示する
- [x] 詳細画面の操作ヘルプを表示する
- [x] viewport本文とヘルプの間に不要な大幅超過がない

### G. スタイルと非カラー環境

- [x] 選択行は色がなくてもカーソルで識別できる
- [x] favoriteは色がなくても`★`で識別できる
- [x] エラー本文はANSIコードを除いても残る
- [x] 各styleが共有状態を実行中に変更しない

## 実装順序

### Step 1: 現状を固定する

1. `go test ./internal/tui` を実行し、既存テストがgreenであることを確認する。
2. 複数画面をまとめた既存テストは残し、新規テストを画面・振る舞い単位に分ける。
3. fakeRendererへ入力本文、幅、呼び出し回数を記録する機能を追加する。

### Step 2: サイズ計算を集約する

最初のTDDケース:

> 幅0・高さ0のWindowSizeMsgを受け取っても、inputとviewportの幅・高さが1以上で、Viewがpanicしない。

1. 上記テストを追加してredを確認する。
2. サイズ正規化とlayout計算を小さな関数へ抽出する。
3. UpdateとViewが同じlayout結果を利用する。
4. 連続リサイズと通常サイズをテストする。

### Step 3: 入力画面の縦横制御を追加する

1. 履歴表示行数のテストを追加する。
2. 選択行が表示範囲へ入る開始indexを計算する。
3. 長いFullNameとエラーの幅制御を追加する。
4. 空・nil・範囲外選択をテストする。
5. ViewがModelを変更しないことを確認する。

### Step 4: 詳細Markdownを安全にする

1. RepoMetaとAnalysisの全項目を一件ずつテストする。
2. nil / 空データをテストする。
3. 表示境界の制御文字除去を追加する。
4. UnicodeとMarkdown記号の扱いを固定する。

### Step 5: Rendererとフォールバックを堅牢化する

1. fakeRendererで本文と幅を検証する。
2. nil rendererのテストを追加する。
3. エラー・空出力のフォールバックを実装する。
4. rendererの生エラーを表示しないことを確認する。

### Step 6: リサイズ時のスクロール維持を実装する

1. 詳細本文を中間位置までスクロールしたModelを作る。
2. リサイズ後もスクロール比率が大きく変わらないテストをredにする。
3. リサイズ専用の再描画処理を追加する。
4. Entry・言語変更では先頭へ戻る既存動作を維持する。
5. 本文が短くなった場合のclampをテストする。

### Step 7: スタイルと各画面を仕上げる

1. 選択行用styleを追加する。
2. 入力・loading・detailの補助情報を共通方針で整える。
3. 色なしでも状態が分かることをテストする。
4. 小さい端末で各画面をスモークテストする。

### Step 8: リファクタリングと全体検証

1. レイアウト計算、文字列無害化、幅制御を責務ごとに分ける。
2. Viewは副作用を持たないことを再確認する。
3. ANSIコードに依存する壊れやすいassertionを避ける。
4. テストリストをすべて完了させる。

各Stepでもテストは必ず一件ずつ red → green → refactor で進める。

## 実装上の注意

- byte数ではなく表示幅を基準にする。
- ANSIエスケープを途中で切断しない。
- Modelが保持するFullNameや解析結果そのものを省略・変更しない。
- Glamourのrenderer生成をViewのたびに無制限に繰り返さない。
- ViewからStore、GitHub、AIを呼ばない。
- テストで利用者の端末種類やカラー設定へ依存しない。
- 既存キー操作と非同期処理のテストを壊さない。

## 完了条件

- TDDテストリストがすべて完了している。
- 3画面が通常端末と極小端末でpanicせず描画できる。
- 履歴が端末高さ内に収まり、選択行が表示される。
- 長いFullName、説明、エラー、ヘルプがレイアウトを破壊しない。
- 詳細Markdownが端末幅に応じて再描画される。
- リサイズ時に詳細の閲覧位置を可能な限り維持する。
- nil Entry / RepoMeta / Analysis / Rendererを安全に扱う。
- 外部由来テキストの危険な制御文字を表示しない。
- 色なしでも選択・favorite・エラーを識別できる。
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

- `internal/tui/view.go`: レイアウト、一覧window、文字列安全化、詳細再描画の補強
- `internal/tui/view_test.go`: 3画面、長文、nil、制御文字、rendererのテスト追加
- `internal/tui/styles.go`: 選択行などのstyle追加
- `internal/tui/update.go`: 共通layout適用とリサイズ時スクロール維持
- `internal/tui/update_test.go`: WindowSizeMsgとスクロール維持のテスト追加
- `docs/plans/feature_tui_view_styles.md`: TDD進捗の更新

## 後続フェーズ

1. `Run`・Cobra・`main.go`のCLI配線
2. 設定ウィザード
3. 実ターミナルでの手動スモークテスト
4. README・GoReleaser・リリース設定
