# Plan: 対話式設定ウィザード

Status: implemented

実装結果（2026-07-22）:

- `LoadStoredConfig` を追加し、保存値と環境変数による実行時上書きを分離した。
- secret非表示・非echo、維持/更新/削除、選択値検証、AI key整合性、確認後の一度だけの保存を実装した。
- EOF/割り込み/入出力エラー、Load/Save失敗では保存せず、安全なメッセージだけを返す。
- `reporepo config` は保存済み設定を読み込む専用経路へ配線した。
- 実装済みシナリオは `internal/core/config_test.go`、`cmd/wizard_test.go`、`cmd/config_test.go` で回帰テストする。

## 前提

- 本計画は `feature-cli-wiring` の実装・統合前に作成している。
- `feature-cli-wiring` では `config` subcommandの枠が作成されているが、現時点では未配線である。
- 実装は `feature-cli-wiring` を `main` へ統合した後、最新 `main` から作成した `feature-config-wizard` worktreeで開始する。
- 実装開始前に `newRootCommand`、CLI依存注入、stdout / stderr、ConfigPathの公開境界を確認し、差異があれば本計画を更新する。
- `feature-tui-view-styles` はCLI配線を通じた間接依存であり、設定ウィザード自身はTUIのView APIへ依存しない。

## 目的

`reporepo config` で設定を安全に対話編集し、`core.Config` として保存できるようにする。

対象設定:

- GitHub token（任意）
- Anthropic API key
- OpenAI API key
- 既定AI provider（`claude` / `openai`）
- 既定言語（`ja` / `en`）

以下を保証する。

- 既存設定を維持しながら一部だけ更新できる。
- API keyとtokenを端末へ再表示しない。
- 対話入力中もsecretをechoしない。
- 環境変数のsecretを設定ファイルへコピーしない。
- 不正なprovider / languageは保存せず再入力させる。
- キャンセル・EOF・入力エラーでは保存しない。
- 保存は確認後に一度だけ行う。
- 保存失敗を安全なCLIエラーとして返す。

## スコープ

### 対象

- `cmd/wizard.go`
- `cmd/wizard_test.go`
- `cmd/root.go` の `config` command配線
- `internal/core/config.go` の保存済み設定読み込み境界
- `internal/core/config_test.go`
- 必要に応じてCLI依存構造の拡張

### 対象外

- API keyのネットワーク疎通確認
- TUI内の設定画面
- OS keychain連携
- Configへのモデル名追加
- 設定ファイル暗号化
- `version` / `where` / `run` の仕様変更
- README・GoReleaser

## 重要な設計判断

### 1. 保存済み設定と実行時設定を分離する

現在の `core.LoadConfig()` は設定ファイルを読み込んだ後、以下の環境変数で値を上書きする。

- `GITHUB_TOKEN`
- `ANTHROPIC_API_KEY`
- `OPENAI_API_KEY`

ウィザードが `LoadConfig()` の戻り値を編集・保存すると、環境変数のsecretを設定ファイルへ書き込む危険がある。そのため、保存済み設定だけを読み込む境界を追加する。

例:

```go
func LoadStoredConfig() (*Config, error)
```

責務:

- `LoadStoredConfig`: ファイルのみ読み込む。環境変数を適用しない。
- `LoadConfig`: `LoadStoredConfig` の結果へ環境変数を適用し、実行時設定を返す。
- `SaveConfig`: 渡された設定を現在どおり0600で保存する。

設定ファイルが存在しない場合、両Load関数は既定provider=`claude`、language=`ja`を持つConfigを返す。

### 2. 対話I/Oを抽象化する

テストで実TTYを使わないよう、入力処理をinterface化する。

例:

```go
type wizardIO interface {
    ReadLine(prompt string) (string, error)
    ReadSecret(prompt string) (string, error)
    Println(message string)
}
```

本番実装:

- 通常入力は `bufio.Reader` を使用する。
- secret入力は端末なら `term.ReadPassword` 相当でechoを無効化する。
- 非TTYのstdinでは行入力へフォールバックするが、入力値をstdout / stderrへ出力しない。
- secret入力後は必要な改行だけを表示する。

テスト実装:

- あらかじめ用意した応答列を順番に返す。
- promptと呼び出し種別を記録する。
- 出力をbufferへ保存する。
- secret値が出力へ含まれないことを検証する。

### 3. secret入力の意味

GitHub / Anthropic / OpenAIの各secret promptは次の規則とする。

| 入力 | 動作 |
|---|---|
| 新しい値 | 新しい値へ更新 |
| 空入力 | 既存値を維持 |
| `-` | 保存済みの値を削除 |

- 既存値そのものをpromptへ表示しない。
- 既存値がある場合は「設定済み」、ない場合は「未設定」とだけ表示する。
- `-` を削除記号としてpromptに明記する。
- secretの前後空白は除去する。
- 空白だけの入力は空入力として扱う。

### 4. 環境変数の扱い

- ウィザードは環境変数の値を読み出してConfigへ代入しない。
- 必要な場合は `os.LookupEnv` 相当で「設定されているか」だけを判定する。
- 環境変数が設定済みなら、保存値より実行時に優先される旨を表示する。
- 出力には環境変数名を表示してよいが、値は表示しない。
- 保存確認のサマリーでは「ファイル設定済み」「環境変数で設定済み」を区別する。

### 5. provider検証

- 入力可能値は `claude` / `openai` のみ。
- 空入力は既存の有効providerを維持する。
- 既存値が不正なら既定値 `claude` を候補として表示する。
- 大文字小文字は正規化せず、明示された小文字だけを受理する。
- 不正値はエラー表示後にproviderだけ再入力させる。
- 選択providerのAPI keyが保存設定にも環境変数にもなければ、理由を表示して再選択またはsecret入力のやり直しへ戻す。
- 少なくともClaudeかOpenAIのどちらか一方が、保存設定または環境変数で利用可能でなければ保存確認へ進まない。

### 6. language検証

- 入力可能値は `ja` / `en` のみ。
- 空入力は既存の有効languageを維持する。
- 既存値が不正なら既定値 `ja` を候補として表示する。
- 不正値はエラー表示後にlanguageだけ再入力させる。

### 7. 確認と保存

保存前にsecretを含まないサマリーを表示する。

例:

```text
GitHub token: 設定済み
Anthropic API key: 設定済み
OpenAI API key: 未設定
既定provider: claude
既定言語: ja
保存しますか? [y/N]
```

- secretは設定有無だけを表示する。
- `y` / `yes` のみ保存する。
- 空入力、`n` / `no` はキャンセルとして正常終了する。
- その他は確認だけ再入力させる。
- キャンセル時は `SaveConfig` を呼ばない。
- 保存成功時は保存先パスまたは「設定を保存しました」を表示する。パス表示はConfigPathと整合させる。
- 保存失敗時はsecretを含まないエラーを返し、成功メッセージを表示しない。

### 8. Cobra command

```text
reporepo config
```

- 位置引数を受け付けない。
- `--help` は設定読込・対話・保存を行わない。
- commandのstdin / stdout / stderrを利用する。
- root commandの依存注入方式に合わせ、LoadStoredConfig / SaveConfig / wizardIO生成を差し替え可能にする。
- `run`、`version`、`where` の依存を初期化しない。
- TUIを起動しない。

## 対話フロー

```text
LoadStoredConfig
  ├─ 失敗 → 安全なエラー、保存なし
  └─ 成功
      → GitHub token
      → Anthropic API key
      → OpenAI API key
      → provider（有効になるまで反復）
      → language（有効になるまで反復）
      → AI key整合性確認
      → secretなしサマリー
      → 保存確認
          ├─ yes → SaveConfig → 成功表示
          └─ no  → キャンセル表示、保存なし
```

任意の入力段階でCtrl-C相当またはEOFを受けた場合はキャンセル扱いとし、保存しない。予期しないI/Oエラーとは区別する。

## エラー方針

- Load失敗: 「保存済み設定を読み込めませんでした」
- 入力I/O失敗: 「設定入力を続行できませんでした」
- Save失敗: 「設定を保存できませんでした」
- 不正provider / language: 許可値を示して同じ項目を再入力
- AI key不足: 必要な環境変数名または再入力方法を案内

生エラー、設定JSON、secret、環境変数値をstdout / stderr / errorへ含めない。

## TDDテストリスト

以下から常に一つだけ選び、実行可能なテストを追加してredを確認してからプロダクトコードを変更する。

### A. 保存済み設定の読み込み境界

- [ ] LoadStoredConfigが設定ファイルだけを読み込む
- [ ] LoadStoredConfigが環境変数を適用しない
- [ ] LoadConfigがLoadStoredConfigへ環境変数を適用する
- [ ] 設定ファイル不在時に既定provider / languageを返す
- [ ] 壊れたJSONを安全にエラーとして返す
- [ ] LoadStoredConfigとLoadConfigが同じConfigPathを利用する
- [ ] LoadConfigの既存テストが維持される

### B. 新規設定

- [ ] 設定ファイルがない状態で有効入力をConfigへ反映する
- [ ] GitHub tokenを空のまま保存できる
- [ ] Anthropic keyだけでclaudeを選択できる
- [ ] OpenAI keyだけでopenaiを選択できる
- [ ] 両AI keyを設定できる
- [ ] providerと言語を正しく保存する
- [ ] SaveConfigを一度だけ呼ぶ
- [ ] 保存成功メッセージを一度表示する

### C. 既存設定の更新

- [ ] secretの空入力で既存値を維持する
- [ ] 新しいsecret入力で既存値を置き換える
- [ ] `-`で既存secretを削除する
- [ ] providerの空入力で既存有効値を維持する
- [ ] languageの空入力で既存有効値を維持する
- [ ] 既存providerが不正ならclaudeを既定候補にする
- [ ] 既存languageが不正ならjaを既定候補にする
- [ ] 更新対象外のフィールドを失わない
- [ ] 読み込み元Configを保存前に直接変更しない

### D. 入力検証

- [ ] 不正providerで保存せずproviderだけ再入力する
- [ ] 不正languageで保存せずlanguageだけ再入力する
- [ ] 確認の不正入力で確認だけ再入力する
- [ ] 選択providerのkey不足を検出する
- [ ] 両AI key不足では保存確認へ進まない
- [ ] 環境変数に対象keyがあればproviderを選択できる
- [ ] secret入力の前後空白を除去する
- [ ] provider / languageの前後空白を除去する

### E. secret保護

- [ ] 既存secretをpromptへ表示しない
- [ ] 入力したsecretをstdoutへ表示しない
- [ ] 入力したsecretをstderrへ表示しない
- [ ] 入力したsecretをerrorへ含めない
- [ ] サマリーは設定有無だけを表示する
- [ ] Load / Save / I/Oエラー時もsecretを表示しない
- [ ] 環境変数の値をConfigへ保存しない
- [ ] 環境変数の値を出力へ表示しない

### F. キャンセルとI/Oエラー

- [ ] 確認で空入力なら保存せず正常終了する
- [ ] 確認で`n` / `no`なら保存しない
- [ ] 確認で`y` / `yes`なら保存する
- [ ] EOFでは保存せずキャンセルする
- [ ] Ctrl-C相当では保存せずキャンセルする
- [ ] 途中の予期しない入力エラーでは保存しない
- [ ] Load失敗ではpromptとSaveを呼ばない
- [ ] Save失敗では成功メッセージを表示しない
- [ ] キャンセル表示にsecretを含めない

### G. Cobra配線

- [ ] root commandがconfigを一度だけ登録する
- [ ] `reporepo config`がwizardを一度呼ぶ
- [ ] configが位置引数を拒否する
- [ ] configのhelpではLoad / prompt / Saveを呼ばない
- [ ] configがTUIを起動しない
- [ ] configがrun用clientを生成しない
- [ ] wizardエラーをCobraへ返す
- [ ] stdout / stderrがcommandのWriterへ出力される

### H. 本番wizardIO

- [ ] 通常入力を一行ずつ読み取る
- [ ] CRLFを正規化する
- [ ] secret入力後に必要な改行を出力する
- [ ] 非TTY入力でもsecretを出力へechoしない
- [ ] EOFをキャンセルとして識別する
- [ ] TTY判定とpassword readerをテストで差し替えられる

## 実装順序

### Step 1: CLI配線との境界を確定する

1. `feature-cli-wiring` を `main` へ統合する。
2. 最新 `main` から `feature-config-wizard` worktreeを作成する。
3. root commandの依存注入とconfig placeholderを確認する。
4. 本計画との差異を更新する。

### Step 2: LoadStoredConfigをTDDで追加する

最初のTDDケース:

> 設定ファイルとANTHROPIC_API_KEY環境変数の両方がある場合、LoadStoredConfigはファイルの値を返し、環境変数の値を返さない。

1. 上記テストを追加してredを確認する。
2. ファイル読み込みと環境変数上書きを分離する。
3. LoadConfigの既存挙動を維持する。
4. 不在・壊れたJSON・既定値を一件ずつ実装する。

### Step 3: wizardの純粋な編集フローを作る

1. fake wizardIOとfake loader / saverを用意する。
2. 新規Configへ有効入力を反映するテストを追加する。
3. この段階では確認と保存を最小実装する。
4. 入力Configを直接変更せず、コピーを編集する。

### Step 4: secret更新規則を実装する

1. 空入力による維持を一項目ずつテストする。
2. 新しい値への置換をテストする。
3. `-`による削除をテストする。
4. prompt / summary / errorへsecretが出ないことをテストする。

### Step 5: provider / language検証を実装する

1. providerの有効値と再入力をテストする。
2. languageの有効値と再入力をテストする。
3. 既存不正値のfallbackを実装する。
4. API keyとproviderの整合性を実装する。
5. 環境変数は存在だけを考慮する。

### Step 6: 確認・キャンセル・エラーを実装する

1. secretなしサマリーをテストする。
2. yes / no / 空 / 不正確認を一件ずつ実装する。
3. EOF / Ctrl-C / I/Oエラーを実装する。
4. Load / Save失敗時に後続処理を止める。

### Step 7: Cobraへ配線する

1. config commandの引数検証を追加する。
2. wizardを一度だけ呼ぶ。
3. commandのstdin / stdout / stderrをwizardIOへ渡す。
4. helpの副作用なし、TUI非起動を確認する。

### Step 8: 本番wizardIOを実装する

1. 通常行入力を実装する。
2. secret非echo入力を実装する。
3. 非TTYfallbackを実装する。
4. CRLF / EOF / 改行をテストする。

### Step 9: リファクタリングと全体検証

1. prompt、検証、Config編集、保存を責務ごとに分ける。
2. secretを受け取る関数の範囲を最小化する。
3. テストごとに新しいCobra commandとfake IOを生成する。
4. テストリストをすべて完了させる。

各Stepでもテストは必ず一件ずつ red → green → refactor で進める。

## 実装上の注意

- `LoadConfig()` の結果をウィザード保存へ使わない。
- secretを `%v`、`%+v`、JSON、ログへ出力しない。
- Config全体をエラーメッセージへ含めない。
- password入力をテストするために実TTYを要求しない。
- グローバルstdin / stdoutを直接参照する範囲を本番adapterへ限定する。
- 保存前の編集はConfigコピーで行う。
- キャンセル時はファイルのmtimeも変更しない。
- 環境変数を変更・削除しない。
- 既存の0600・アトミック保存を `core.SaveConfig` へ委譲する。
- `run`、`version`、`where` のテストを壊さない。

## 完了条件

- `reporepo config`で全設定を対話編集できる。
- 既存値の維持・置換・削除が仕様どおり動作する。
- provider / language / API key整合性を保存前に検証する。
- キャンセル・EOF・エラーで保存しない。
- 環境変数のsecretを設定ファイルへコピーしない。
- secretがprompt、summary、stdout、stderr、errorへ出ない。
- 保存成功時だけ成功メッセージを表示する。
- Configは既存の0600・アトミック保存を利用する。
- テストが実TTY、network、利用者homeへ依存しない。
- 変更したGoファイルが`gofmt`済みである。
- 以下がすべて成功する。

```bash
go test ./cmd ./internal/core
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

手動確認は専用の一時HOME / XDG_CONFIG_HOMEを使用し、実API keyは入力しない。

## 想定される変更

- `cmd/wizard.go`: wizardフロー、入力検証、secretサマリー
- `cmd/wizard_test.go`: 入力列、secret保護、キャンセル、保存のテスト
- `cmd/root.go`: config commandと依存の配線
- `cmd/root_test.go`: config登録・副作用なしのテスト
- `internal/core/config.go`: LoadStoredConfigとLoadConfigの責務分離
- `internal/core/config_test.go`: ファイル値と環境変数値の分離テスト
- `docs/plans/feature_config_wizard.md`: TDD進捗の更新

## 後続フェーズ

1. CLI + Config + TUIの実ターミナル統合スモークテスト
2. READMEの設定手順と環境変数優先順位
3. GoReleaserとversion ldflags
4. クロスコンパイル・リリース検証
