# Plan: Cobra CLIプレゼンテーション

Status: implemented

## 目的

Cobraが表示するhelp、version、保存先、設定ウィザード、成功、警告、キャンセル、エラーを統一した見た目と出力規則へ移行する。

TTYではLip Glossによる色・太字・余白・状態記号を利用し、非TTY、`NO_COLOR`、`TERM=dumb`ではANSIを含まないplain textを出力する。表示改善によって既存commandの意味、終了コード、secret管理、pipe利用を壊さない。

## 前提

- `SPEC.md` 2.8と非機能要件を正とする。
- Cobra commandは `run`、`config`、`version`、`where` を持つ。
- フルスクリーン画面は `internal/tui` が担当し、本計画の対象外とする。
- Lip Glossは既に依存済みであり、新しいUI libraryは追加しない。
- `config` はstdin/stdoutを差し替え可能で、secret入力時のecho抑止を実装済みである。
- mainは `cmd.Execute` のerrorを終了コード1へ変換している。

## スコープ

### 対象

- CLI専用presentation package
- TTY／非TTY／`NO_COLOR`／`TERM=dumb`判定
- semantic roleに基づく表示
- Cobra help／usage／example
- version／whereのTTY表示とplain出力
- config wizardの段階表示、prompt、summary、結果表示
- warning／errorのstderr出力
- errorの一度だけの描画と終了コード維持
- terminal幅を考慮したsummary
- ANSI、secret、stdout/stderrの回帰テスト

### 対象外

- Bubble Tea TUIのstyle変更
- application解析中のspinner変更
- commandやflagの追加
- JSON出力flagなどの機械可読format追加
- shell completionの装飾
- Windows固有console APIの直接実装
- localization frameworkの導入

## 表示契約

### Semantic role

各commandは色やANSIを直接指定せず、次の意味だけをpresentation層へ渡す。

```text
Title
Section
Label
Value
Success
Warning
Error
Hint
Muted
Prompt
```

decorated modeの初期表現:

| role | 表現 |
|---|---|
| Title / Section | 太字、primary color |
| Label | muted color |
| Value | 通常色または強調 |
| Success | `✓` とgreen |
| Warning | `⚠` とyellow |
| Error | `✗` とred |
| Hint / Muted | dim color |
| Prompt | primary color、入力部分は通常色 |

plain modeでは色とANSIを使わず、成功、警告、失敗が文言だけで判別できるprefixを付ける。

```text
OK: 設定を保存しました
WARNING: OpenAI API keyは設定されていません
ERROR: 設定を保存できませんでした
```

日本語本文は維持し、prefixは固定する。テストとpipe利用が不安定になるため、terminalやlocaleに応じて記号・文言を自動変更しない。

### stdout / stderr

- help、version、where、prompt、summary、成功、キャンセルはstdout。
- warning、validation error、command errorはstderr。
- wizardの入力再試行メッセージは利用者が同じ画面で追えるようstderrへ出す。
- Cobraへ設定されたwriterを利用し、`os.Stdout`／`os.Stderr`へ直接書かない。
- 書き込みerrorは呼び出し元へ返す。
- secret、backend raw error、利用者homeを含み得る下位errorを描画へ渡さない。

### terminal capability

decorated modeはメッセージごとの出力先について次をすべて満たす場合だけ有効にする。

1. writerがterminalである。
2. `NO_COLOR` が存在しない。
3. `TERM` が `dumb` ではない。

stdoutとstderrを個別判定する。stdinがTTYかどうかはsecret echo抑止と対話可否にだけ利用し、出力装飾の判定には使わない。

判定結果はconstructorへ注入できる値へ変換し、renderer自身がテスト中に実terminalやglobal環境変数を読まない。

### terminal幅

- 幅が不明または0以下なら80を既定値とする。
- 幅40未満ではsummaryを `label: value` の縦並びにする。
- 幅40以上ではlabel列を揃える。
- path、version、provider、secret状態をtruncateしない。
- box borderは必須にしない。内容が幅を超える場合は装飾より情報を優先する。

## パッケージ設計

```text
internal/presentation/
├── renderer.go
├── styles.go
├── terminal.go
├── renderer_test.go
└── terminal_test.go
```

### Capabilities

```go
type Capabilities struct {
    Decorated bool
    Width     int
}
```

本番用resolverはwriter、`NO_COLOR`、`TERM`、terminal sizeからCapabilitiesを作る。テストはCapabilitiesを直接指定する。

### Renderer

最初は必要最小限のAPIから開始する。

```go
type Renderer struct {
    out  io.Writer
    caps Capabilities
}

func NewRenderer(out io.Writer, caps Capabilities) *Renderer
func (r *Renderer) Title(text string) error
func (r *Renderer) Section(text string) error
func (r *Renderer) Success(text string) error
func (r *Renderer) Warning(text string) error
func (r *Renderer) Error(text string) error
func (r *Renderer) Hint(text string) error
func (r *Renderer) Summary(rows []Row) error
func (r *Renderer) Prompt(label string) error
```

実装時は利用箇所のredテストに必要なmethodだけを一つずつ追加する。roleごとにmethodが増えすぎる場合は内部の `write(Role, string)` へ集約するが、command側へstyleを公開しない。

### Row

```go
type Row struct {
    Label string
    Value string
}
```

Rowは表示用データだけを持つ。secret値や設定保存処理は保持しない。

### Style

- `internal/tui/styles.go` をimportしない。
- Lip Gloss styleはpackage内のprivate値とする。
- testではcolor profileに左右されないようdecorated modeを明示し、ANSIの存在とstrip後の本文を別々に検証する。
- style適用前に利用者入力やpathをescape sequenceとして解釈しない。

## Cobra統合

### command dependencies

`commandDependencies` にpresentation factoryまたはcapability resolverを注入する。rendererをroot作成時に固定すると、その後の `root.SetOut`／`SetErr` を無視するため、各RunEまたはhelp描画時に `cmd.OutOrStdout()`／`cmd.ErrOrStderr()` から生成する。

```go
type presenterFactory func(io.Writer) *presentation.Renderer
```

本番factoryはwriterごとのterminal capabilityを解決する。テストfactoryはdecorated/plainを固定する。

### error ownership

- rootは `SilenceUsage: true` と `SilenceErrors: true` にする。
- RunE以下は利用者向けのsanitized errorを返し、直接最終errorを描画しない。
- `Execute` が返却errorを一度だけstderrへ描画する。
- mainは従来どおり終了コードだけを決め、errorを再表示しない。
- writerへの描画自体が失敗した場合もpanicせず、元errorを失わない。

これによりCobra既定出力、RunE、mainによる二重表示を防ぐ。

### help

- Cobraのcommand／flag metadataを正とする。
- rootにLong、Exampleを追加する。
- help templateまたはhelp functionはCobra metadataから次を描画する。
  1. titleと概要
  2. usage
  3. available commands
  4. flags
  5. examples
  6. `reporepo [command] --help` のhint
- subcommand helpも同じroleを使う。
- 非TTYではANSIなしでCobra標準に近い安定したplain textを返す。
- help表示はcommand実行、設定読み込み、SecretStoreアクセスを行わない。

### version

- 非TTYでは現在の `reporepo 0.1.0` を維持する。
- TTYではtitle／labelを付けてもversion文字列自体は変更しない。
- trailing newlineを一つにする。

### where

- 非TTYでは現在の2行を維持する。

```text
config: <path>
data: <path>
```

- TTYではsectionと整列したsummaryを利用できる。
- pathをtruncate、quote、色コード混入させない。
- path解決errorは既存のsanitized messageを維持する。

## 設定ウィザード統合

### wizardIO

現在の `ReadLine`、`ReadSecret`、`Println` へ無秩序にstyle文字列を渡さない。次のいずれかの最小設計をredテストで選ぶ。

1. `wizardIO` にSection／Warning／Summary／Success等のsemantic methodを追加する。
2. 入力用IOと出力用presenterを `wizardDependencies` で分離する。

推奨は2とする。secret echo抑止と行入力はconsole IO、意味のある出力はpresentation Rendererが担当できる。

### 表示順

```text
Reporepo configuration

Secrets
  GitHub token       Keychain設定済み
  Anthropic API key  未設定
  OpenAI API key     未設定
  Gemini API key     環境変数

Defaults
  Provider  gemini
  Language  ja

Review
  ...

保存しますか? [y/N]:
```

- secret入力前の状態と保存予定summaryを区別する。
- secret入力値はrendererへ渡さない。
- promptはTTYでもsecretの存在状態だけを含む。
- validation errorには有効な選択肢と再入力promptを表示する。
- environment warningは該当secretごとに一度だけ表示する。
- 保存成功、cancel、rollbackを意味の異なるroleで表示する。
- EOFとsignal cancelは従来どおり安全に終了し、不要なerror表示を行わない。

## TDDテストリスト

実装時は次のリストから一件だけを選び、red → green → refactorを繰り返す。

### A. terminal capability

- [ ] TTY writerでdecoratedになる
- [ ] 非TTY writerでplainになる
- [ ] `NO_COLOR` の存在でplainになる
- [ ] `NO_COLOR` が空文字でもplainになる
- [ ] `TERM=dumb` でplainになる
- [ ] stdoutとstderrを別々に判定する
- [ ] terminal幅を取得できる
- [ ] 幅取得失敗時に既定幅80を使う
- [ ] fake判定だけでテストでき、実terminalを使わない

### B. Renderer基本契約

- [ ] decorated Successが記号、本文、ANSIを含む
- [ ] plain Successが `OK:` と本文を含みANSIを含まない
- [ ] decorated Warningが記号、本文、ANSIを含む
- [ ] plain Warningが `WARNING:` と本文を含みANSIを含まない
- [ ] decorated Errorが記号、本文、ANSIを含む
- [ ] plain Errorが `ERROR:` と本文を含みANSIを含まない
- [ ] Title／Section／Hintのstrip後本文がmode間で一致する
- [ ] 各methodがwriter errorを返す
- [ ] message内のANSI風入力を安全に扱う

### C. Summary

- [ ] 幅40以上でlabel列が揃う
- [ ] 幅40未満で縦並びになる
- [ ] plain summaryにANSIを含まない
- [ ] 長いpathをtruncateしない
- [ ] 空rowでもpanicしない
- [ ] secret状態だけを表示し値を含まない

### D. Cobra help

- [ ] root helpに概要、usage、4 command、exampleを含む
- [ ] subcommand helpにusageと説明を含む
- [ ] decorated helpにANSIを含む
- [ ] plain helpにANSIを含まない
- [ ] helpがrun／loadConfig／SecretStoreを呼ばない
- [ ] command metadata変更がhelpへ反映される
- [ ] unknown commandでusage全文を表示しない
- [ ] unknown commandでhelp案内を表示する

### E. error ownership

- [ ] rootが `SilenceUsage` と `SilenceErrors` を有効にする
- [ ] RunE errorをstderrへ一度だけ表示する
- [ ] error時の終了コードが1のままである
- [ ] success時の終了コードが0のままである
- [ ] sanitized error本文を維持する
- [ ] backend raw errorとsecretをstderrへ含めない
- [ ] stderrが非TTYならANSIを含めない
- [ ] error renderer失敗時にpanicしない

### F. version／where

- [ ] plain versionが従来の1行を維持する
- [ ] decorated versionがversion番号を正確に含む
- [ ] plain whereが従来の2行を維持する
- [ ] decorated whereが2 pathを正確に含む
- [ ] pathをtruncateしない
- [ ] path解決errorをstderrへ一度だけ表示する
- [ ] 成功時にstderrへ出力しない

### G. Wizard構造

- [ ] titleとSecrets／Defaults／Review sectionを順に表示する
- [ ] secretの初期状態をsummary表示する
- [ ] 保存予定providerと言語をsummary表示する
- [ ] environment優先をWarningまたはHintで表示する
- [ ] invalid providerをstderrへ表示して再入力する
- [ ] invalid languageをstderrへ表示して再入力する
- [ ] 保存成功をSuccessで表示する
- [ ] 保存拒否をcancel表示にする
- [ ] EOFでは保存せず不要なErrorを表示しない

### H. Wizard security／回帰

- [ ] decorated outputへ既存secretを含めない
- [ ] plain outputへ既存secretを含めない
- [ ] 新規入力secretをstdout／stderrへ含めない
- [ ] environment secretをstdout／stderrへ含めない
- [ ] rollback errorへsecretを含めない
- [ ] TTY secret入力のecho抑止を維持する
- [ ] 非TTY入力で従来どおり行入力できる
- [ ] keep／set／delete／rollbackの既存挙動を維持する

### I. 横断回帰

- [ ] 全commandのplain出力にANSIがない
- [ ] commandごとのstdout／stderr契約を満たす
- [ ] `NO_COLOR=1 reporepo --help` がplainになる
- [ ] `TERM=dumb reporepo config` がplainになる
- [ ] redirect時にspinnerやcarriage returnを出さない
- [ ] Windows／Linux／macOS向けbuildが成功する
- [ ] race testが成功する

## 実装順序

### Step 1: Capabilities

1. 非TTYのredテストを書く。
2. injectableなTTY／env／width resolverを追加する。
3. `NO_COLOR` と `TERM=dumb` を一件ずつ追加する。
4. stdout／stderrの独立判定を確認する。

### Step 2: 最小Renderer

1. plain Successのredテストを書く。
2. decorated Successを追加する。
3. Warning、Errorを一roleずつ追加する。
4. Title、Section、Hintを利用箇所に合わせて追加する。
5. writer errorとANSI風入力を確認する。

### Step 3: Summary

1. 幅40以上の整列を実装する。
2. 狭い幅の縦並びを実装する。
3. 長いpathとsecret状態を使って非欠落を確認する。

### Step 4: Cobra factoryとerror ownership

1. `SetOut` 後のwriterを使うredテストを書く。
2. presenter factoryをdependencyへ追加する。
3. `SilenceErrors` を有効化する。
4. Executeでerrorを一度だけ描画する。
5. mainの終了コード回帰を確認する。

### Step 5: version／where

1. plain出力のcharacterization testを固定する。
2. TTY表示をRendererへ移す。
3. stdout／stderr、path非欠落を確認する。

### Step 6: Help

1. root helpの必要情報をredテストにする。
2. Long／Exampleと共通help rendererを追加する。
3. subcommand helpへ適用する。
4. unknown command／flag errorの案内を追加する。
5. helpが外部依存へ触れないことを確認する。

### Step 7: Wizard出力境界

1. wizard IOからpresentationを分離する。
2. titleとsectionを一つずつ追加する。
3. 状態summaryをRowへ変換する。
4. warning／validation／success／cancelをroleへ移行する。
5. secret非漏洩テストをdecorated／plain両方で実行する。

### Step 8: 統合とリファクタリング

1. command横断のANSI／stdout／stderrテストを追加する。
2. 重複した文字列組み立てとstyleをpresentationへ集約する。
3. 実terminalで狭幅、`NO_COLOR`、redirectを確認する。
4. SPECとREADMEのCLI例を実装結果へ同期する。

各Stepでもテストは必ず一件ずつred → green → refactorで進める。

## 完了条件

- help、version、where、config、warning、errorが共通semantic styleを使う。
- TTYでは階層と状態を視覚的に判別できる。
- 非TTY、`NO_COLOR`、`TERM=dumb`ではANSIを一切出力しない。
- plain version／whereの既存出力契約を維持する。
- command errorをstderrへ一度だけ表示する。
- command error時は終了コード1、成功時は0を維持する。
- terminal幅が狭くても情報、path、状態を欠落させない。
- stdout、stderr、errorへsecretを含めない。
- presentation層がTUI、Config、SecretStore、外部clientへ依存しない。
- 自動テストが実terminalや実Keychainへ依存しない。
- 次のコマンドがすべて成功する。

```bash
gofmt -w internal/presentation cmd main.go main_test.go
go test ./internal/presentation ./cmd .
go test ./...
go test -race ./...
go vet ./...
CGO_ENABLED=0 go build ./...
make cross
```

## 想定される変更

- `internal/presentation/renderer.go`: semantic renderingとsummary
- `internal/presentation/styles.go`: CLI専用Lip Gloss style
- `internal/presentation/terminal.go`: TTY、env、terminal幅の解決
- `internal/presentation/*_test.go`: decorated／plain／capabilityテスト
- `cmd/root.go`: presenter factory、help、version、where、error ownership
- `cmd/root_test.go`: help、stdout/stderr、ANSI、error回帰
- `cmd/wizard.go`: input IOとpresentationの分離
- `cmd/wizard_test.go`: section、summary、warning、security回帰
- `cmd/config_test.go`: Cobra経由のconfig出力回帰
- `main.go` / `main_test.go`: error描画と終了コードの責務確認
- `SPEC.md` / `README.md`: 実装結果とCLI例の同期

## 手動スモークテスト

ダミーsecretまたはsecret未設定状態で実施し、実secretを画面記録へ含めない。

```bash
reporepo --help
reporepo version
reporepo where
reporepo config
NO_COLOR=1 reporepo --help
TERM=dumb reporepo config
reporepo where | sed -n '1,2p'
reporepo unknown-command
```

次を目視確認する。

1. 通常TTYで見出し、状態、hintが判別できる。
2. `NO_COLOR` と `TERM=dumb` でescape sequenceが出ない。
3. pipe出力が1行単位で安定している。
4. 狭いterminalでもpathや設定状態が欠落しない。
5. unknown commandのerrorが一度だけ表示される。
6. wizardのsecret入力がechoされず、summaryにも値が出ない。
