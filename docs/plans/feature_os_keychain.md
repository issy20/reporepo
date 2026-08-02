# Plan: OS Keychainによるsecret管理

Status: implemented（OS別手動スモークテストを除く）

## 目的

GitHub token、Anthropic API key、OpenAI API keyを平文の `config.json` からOSの資格情報ストアへ移し、環境変数を最優先とする安全な実行時設定を構築する。

対象OSとbackend:

| OS | backend |
|---|---|
| macOS | Keychain |
| Windows | Credential Manager |
| Linux / *BSD | Secret Service over D-Bus |

以下を保証する。

- 新規保存する `config.json` にsecretを含めない。
- secretはOS資格情報ストアへ保存する。
- 環境変数をOS資格情報ストアより優先する。
- 環境変数の値をKeychainやJSONへ書き戻さない。
- Keychain障害時に平文保存へ自動フォールバックしない。
- 設定ウィザードの途中失敗時に可能な範囲でロールバックする。
- 旧形式JSONのsecretを安全かつ冪等に移行する。
- stdout、stderr、error、ログへsecretを含めない。
- 単体・統合テストでは `internal/testutil.MemorySecretStore` を注入し、実OS Keychainを読み書きしない。

## 前提

- `SPEC.md` 2.7、3、4.5、4.6、5.1、5.2、6.3を正とする。
- 現在の `core.Config` は実行時設定とJSON保存形式を兼ねている。
- 現在の設定ウィザードはsecretを `Config` へ設定して `SaveConfig` する。
- 現在のapplication runnerは `core.LoadConfig` から直接secretを受け取る。
- 現在の `config.json` にはsecretが平文で存在する可能性がある。
- `feature-os-keychain` は `main` のコミット `a54b410` を起点とする。

## スコープ

### 対象

- `internal/secretstore/store.go`
- `internal/secretstore/keyring.go`
- 対応するテスト
- `internal/core/config.go` とテスト
- `cmd/application.go` とテスト
- `cmd/wizard.go` とテスト
- `cmd/root.go` の依存配線
- 旧形式設定の移行処理とテスト
- `go.mod` / `go.sum`
- 必要なドキュメント更新

### 対象外

- OS keychainの独自暗号実装
- 平文ファイルbackend
- クラウド型secret manager
- 1Password / Bitwarden固有連携
- モデル名・API仕様の更新
- GitHub / Claude / OpenAIクライアント内部の変更
- TUIの画面・キー操作変更

## 用語と識別子

### Service

```text
reporepo
```

### Account

```text
github-token
anthropic-api-key
openai-api-key
```

### 環境変数

```text
GITHUB_TOKEN
ANTHROPIC_API_KEY
OPENAI_API_KEY
```

識別子は一箇所へ定数化し、wizard、application、migrationで文字列を重複定義しない。

## アーキテクチャ

```text
cmd (composition root)
├── core.ConfigFile（非secret設定）
├── secretstore.Store（OS資格情報ストア）
├── environment（最優先上書き）
└── runtime core.Config
      └── GitHub / Claude / OpenAI / TUI
```

`internal/core` はkeyringライブラリへ依存しない。`internal/secretstore` だけがOS keyring adapterを知り、`cmd` が両者を組み立てる。

## 1. SecretStore境界

### interface

```go
package secretstore

type Key string

const (
    GitHubToken     Key = "github-token"
    AnthropicAPIKey Key = "anthropic-api-key"
    OpenAIAPIKey    Key = "openai-api-key"
)

var ErrNotFound = errors.New("secret not found")

type Store interface {
    Get(Key) (string, error)
    Set(Key, string) error
    Delete(Key) error
}
```

設計規則:

- `Get` は未登録時だけ `ErrNotFound` を返す。
- backend障害は `ErrNotFound` と区別する。
- `Set` は空文字を受け付けない。削除は `Delete` を使用する。
- `Delete` は未登録時も成功として扱える冪等な操作にする。
- 無効なKeyを拒否する。
- errorにsecret値を含めない。

### Keyring adapter

初期実装では `github.com/zalando/go-keyring` をadapter内部で利用する。

```go
type keyringBackend interface相当 {
    Get(service, account string) (string, error)
    Set(service, account, secret string) error
    Delete(service, account string) error
}
```

ライブラリがpackage関数を提供する場合は、関数フィールドを持つadapter constructorを非公開で用意し、テストで差し替える。

変換規則:

- ライブラリのnot found → `secretstore.ErrNotFound`
- その他のGet / Set / Delete失敗 → secretを含まない操作別エラー
- Deleteのnot found → nil

本番constructorだけがservice=`reporepo`を使用する。

## 2. Configの保存形式分離

### 実行時Config

`core.Config` は既存クライアントとの互換性のためsecretフィールドを保持するが、JSONから除外する。

```go
type Config struct {
    GithubToken     string `json:"-"`
    AnthropicAPIKey string `json:"-"`
    OpenAIAPIKey    string `json:"-"`
    DefaultProvider string `json:"default_provider"`
    DefaultLanguage string `json:"default_language"`
}
```

### ファイル読み込み

旧形式検出のため、非公開DTOを使用する。

```go
type configFile struct {
    GithubToken     string `json:"github_token,omitempty"`
    AnthropicAPIKey string `json:"anthropic_api_key,omitempty"`
    OpenAIAPIKey    string `json:"openai_api_key,omitempty"`
    DefaultProvider string `json:"default_provider"`
    DefaultLanguage string `json:"default_language"`
}

type LegacySecrets struct {
    GithubToken     string
    AnthropicAPIKey string
    OpenAIAPIKey    string
}
```

新しい読み込み境界を用意する。

```go
func LoadConfigFile() (*Config, LegacySecrets, error)
```

- `Config` には非secret設定だけを設定する。
- 旧JSONのsecretは `LegacySecrets` に分離する。
- ファイル不在時は既定provider / languageと空LegacySecretsを返す。
- `LoadStoredConfig` を残す場合は `LoadConfigFile` を利用してLegacySecretsを捨てる。
- `SaveConfig` は新形式DTOだけをmarshalする。
- `Config`を直接 `json.Marshal` してもsecretが出ない。

## 3. 実行時secret解決

secretごとの解決順序:

1. 対応する環境変数が空白以外なら使用する。
2. 環境変数がなければSecretStoreから取得する。
3. `ErrNotFound` なら未設定とする。
4. backend障害は利用可能な他providerとsecretの必須性を考慮して扱う。

### エラー・縮退規則

- Anthropic / OpenAIの両方が解決できなければ起動エラー。
- 一方のAI secretが解決でき、他方のGetだけがbackend障害なら、利用可能なproviderで起動し、安全な警告を表示できる境界を用意する。
- GitHub tokenは任意。Get失敗時はtokenなしで起動可能だが、SecretStore障害を黙殺せず安全な警告へ変換する。
- 対象環境変数が存在するsecretはKeychainを読まない。Keychainが利用不能なヘッドレス環境でも環境変数だけで実行可能にする。
- DefaultProviderは解決済みAI mapに存在するproviderへ補正する。
- 読み込んだConfigと設定ファイルをsecret解決時に書き換えない。

警告出力の具体的な配線が大きくなる場合は、loaderが `[]string` の安全な警告を返し、CLIがstderrへ表示する。下位errorはそのまま出さない。

## 4. 設定ウィザード

### 読み込み

- 非secret設定は `LoadConfigFile` から読む。
- 旧secretがある場合は先に移行を行う。
- 既存secretの値はSecretStoreから読み、メモリ上だけで保持する。
- 環境変数は設定有無だけを確認し、値を読み込み・保存対象にしない。
- Keychain backend障害時はwizardを開始せず、安全なエラーを返す。

### 入力

現在の仕様を維持する。

- 空入力: Keychain上の既存値を維持
- 新しい値: Set候補
- `-`: Delete候補
- 値はecho・再表示しない
- サマリーは「Keychain設定済み / 環境変数で設定済み / 未設定」の状態だけを表示

単なる文字列だけでなく、更新意図を保持する。

```go
type secretAction uint8

const (
    keepSecret secretAction = iota
    setSecret
    deleteSecret
)

type secretEdit struct {
    action secretAction
    value  string
}
```

これにより「未設定の空文字」と「既存値を維持」を区別する。

### 保存トランザクション

1. 保存前に対象Keyの旧状態をsnapshotする。
2. secret編集を決められた順序で適用する。
3. すべてのsecret操作成功後に `SaveConfig` で非secret設定を保存する。
4. すべて成功したら成功メッセージを表示する。

失敗時:

- secret操作失敗: 適用済み操作を逆順でsnapshotへ戻す。
- Config保存失敗: 全secret操作を逆順でsnapshotへ戻す。
- rollback失敗: 元のエラーに加え、安全な復旧案内を返す。
- rollback errorやsecret値は表示しない。
- 保存確認前のcancel / EOF / I/O errorでは何も変更しない。

## 5. 旧形式移行

### 起動条件

`LoadConfigFile` が1つ以上のLegacySecretsを返した場合に実行する。application起動とconfig wizardの両方が同じmigration関数を利用する。

### 優先規則

- Keychainに同じKeyが未登録なら旧JSONの値をSetする。
- Keychainに既存値がある場合はKeychainを正として上書きしない。
- 空のlegacy値は無視する。
- 環境変数は移行元・移行先にしない。

### 移行手順

1. 旧JSONを読み取る。
2. 各legacy KeyについてKeychainの現状を取得する。
3. 未登録のKeyだけをSetする。
4. 全KeyがKeychainに存在する状態を確認する。
5. secretを含まない `config.json` をアトミック保存する。
6. 成功を返す。

### 失敗時

- JSONは書き換えない。
- 今回新規SetしたKeyは可能なら削除してrollbackする。
- 元から存在したKeyは変更しない。
- rollback失敗でもJSONは保持する。
- 次回の再実行を許容する。
- 通常実行でlegacy値をsecretとして黙って使用しない。
- エラーにはsecretを含めず、Keychain利用可否の確認と環境変数による一時実行方法を案内する。

移行関数は同じ入力で複数回呼んでも結果が変わらない冪等性を持つ。

## 6. OS別動作

### macOS

- Keychainを利用する。
- 初回アクセスや設定変更でOSの許可UIが表示され得る。
- service/accountの組み合わせが安定していることを手動確認する。

### Windows

- Credential Managerを利用する。
- Windows向けcross buildがCGOなしで成功することを確認する。
- 実保存・取得・削除はWindows環境の手動またはCIスモークテストへ分離する。

### Linux / *BSD

- Secret Service over D-Busを利用する。
- desktop環境では保存・取得・削除を確認する。
- D-Busなし、serviceなし、locked collectionをbackend障害として扱う。
- ヘッドレス環境では環境変数だけでapplicationを起動できる。
- wizardはKeychainがなければ平文保存せずエラーにする。

## TDDテストリスト

以下から常に一つだけ選び、実行可能なテストを追加してredを確認してからプロダクトコードを変更する。

### A. SecretStore境界

- [x] Key定数が仕様のaccount名と一致する
- [x] 有効KeyのGet / Set / Deleteをbackendへ委譲する
- [x] service名として`reporepo`を渡す
- [x] backendのnot foundをErrNotFoundへ変換する
- [x] Getのbackend障害をnot foundと区別する
- [x] Deleteのnot foundを成功として扱う
- [x] 空secretのSetを拒否する
- [x] 無効Keyを拒否する
- [x] adapter errorにsecretを含めない
- [x] fake backendで実OS Keychainへ触れずテストできる

### B. Config JSON

- [x] Configをmarshalしても3つのsecret名と値を含まない
- [x] SaveConfigがprovider / languageだけを保存する
- [x] 新形式Configを読み込む
- [x] 旧形式ConfigからLegacySecretsを分離する
- [x] 旧secretをruntime Configへ直接設定しない
- [x] Config file不在時に既定値と空LegacySecretsを返す
- [x] 壊れたJSONをエラーにする
- [x] 0600・アトミック保存を維持する
- [x] 既存のConfig環境変数テストを新しい責務へ移行する

### C. 実行時secret解決

- [x] 環境変数がKeychainより優先される
- [x] 環境変数があるKeyはKeychain Getを呼ばない
- [x] 環境変数がないKeyをKeychainから取得する
- [x] ErrNotFoundを未設定として扱う
- [x] 両AI secret未設定で起動しない
- [x] Claudeだけでproviderをclaudeへ補正する
- [x] OpenAIだけでproviderをopenaiへ補正する
- [x] 両AI secretで有効providerを維持する
- [x] GitHub secret未設定でtokenなし起動する
- [x] 環境変数の値をKeychainへSetしない
- [x] 解決後にConfig fileを書き換えない
- [x] backend障害の生エラーとsecretを表示しない
- [x] ヘッドレス相当のbackend障害でもAI環境変数だけで起動する

### D. Wizard読み込み・表示

- [x] Wizardが非secret設定とKeychain secretを別々に読む
- [x] Keychain secret値をpromptへ表示しない
- [x] サマリーがKeychain設定済みを表示する
- [x] 環境変数は存在状態だけを表示する
- [x] 環境変数の値を編集対象にしない
- [x] Keychain Get障害時にpromptを開始しない
- [x] wizard errorへsecretを含めない

### E. Wizard編集・保存

- [x] 空入力をkeep actionにする
- [x] 新値をset actionにする
- [x] `-`をdelete actionにする
- [x] keepではSet / Deleteを呼ばない
- [x] setではKeychainへ新値を保存する
- [x] deleteではKeychainから削除する
- [x] secret保存後に非secret Configを保存する
- [x] Config JSONに入力secretを含めない
- [x] 成功時だけ成功メッセージを表示する
- [x] cancel / EOFでKeychainとConfigを変更しない

### F. Wizard rollback

- [x] 2つ目のsecret Set失敗で1つ目を元へ戻す
- [x] Delete後の後続失敗で削除値を復元する
- [x] Config保存失敗で全secretを元へ戻す
- [x] 元が未登録のSetをrollback時にDeleteする
- [x] 元が登録済みのSetをrollback時に旧値へ戻す
- [x] rollbackを逆順で行う
- [x] rollback失敗を安全な復旧エラーにする
- [x] rollback errorにsecretを含めない

### G. 旧形式移行

- [x] legacyがなければKeychainとConfig Saveを呼ばない
- [x] 未登録Keyへlegacy secretをSetする
- [x] 既存Keyをlegacy値で上書きしない
- [x] 空legacy secretを無視する
- [x] 全secret成功後に新形式Configを保存する
- [x] 移行後JSONにsecret名と値を含めない
- [x] Keychain失敗時に旧JSONを変更しない
- [x] Config保存失敗時に今回SetしたKeyをrollbackする
- [x] 元から存在したKeyをrollbackで変更しない
- [x] 同じ移行を複数回実行しても結果が変わらない
- [x] 移行失敗時にlegacy secretをruntime利用しない
- [x] 移行エラーにsecretを含めない

### H. CLI配線

- [x] root commandが本番SecretStoreをwizardへ渡す
- [x] applicationが同じSecretStoreからruntime secretを解決する
- [x] config / run以外はSecretStoreへアクセスしない
- [x] help / version / whereがKeychain permission UIを発生させない
- [x] Keychain unavailableのwizardが安全なエラーを返す
- [x] Keychain unavailableでも環境変数だけのrunが成功境界へ進む

### I. セキュリティ回帰

- [x] stdoutへsecretを含めない
- [x] stderrへsecretを含めない
- [x] errorへsecretを含めない
- [x] config.jsonへsecret名と値を含めない
- [x] data.jsonへsecretを含めない
- [x] テストfailure messageへ実secretを出さない
- [x] 環境変数をKeychain / JSONへ書き戻さない
- [x] 平文fallback実装が存在しない

### J. ビルド・手動確認

- [x] macOSでKeychain Set / Get / Deleteを手動確認する
- [ ] Linux desktopでSecret Serviceを手動確認する
- [ ] Linux headless相当で安全なエラーを確認する
- [ ] WindowsでCredential Managerを確認する
- [x] darwin / linux / windowsのamd64 / arm64 buildが成功する
- [x] CGO_ENABLED=0で対象buildが成功する

## 実装順序

### Step 1: SecretStore契約

最初のTDDケース:

> backendのGetがnot foundを返した場合、adapterは `secretstore.ErrNotFound` を返し、その他のbackend errorとは区別する。

1. `internal/secretstore` とKey定数を作る。
2. fake backendでGet / Set / Deleteを一件ずつ実装する。
3. error変換、空値、無効Keyを追加する。
4. この段階では本番Keychainを呼ばない。

### Step 2: OS Keyring adapter

1. `github.com/zalando/go-keyring` を依存へ追加する。
2. package関数をadapterへ閉じ込める。
3. ライブラリ固有errorを変換する。
4. 全テストをfake関数で実行する。

### Step 3: Config JSONからsecretを除外

1. Config marshalのredテストを書く。
2. secretフィールドを `json:"-"` にする。
3. configFile DTOとLegacySecretsを追加する。
4. 新旧形式の読み込みを一件ずつ実装する。
5. SaveConfigが非secret項目だけを書くことを確認する。

### Step 4: 実行時secret resolver

1. 環境変数優先のredテストを書く。
2. env / Keychain / not foundをKeyごとに実装する。
3. AI provider補正へ接続する。
4. backend障害とoptional GitHub tokenを実装する。
5. application compositionへ注入する。

### Step 5: Wizardのsecret編集モデル

1. 文字列直接更新をsecretEditへ置き換える。
2. keep / set / deleteを一件ずつテストする。
3. Keychain状態と環境変数状態の表示を分離する。
4. secret値非表示の既存テストを維持する。

### Step 6: Wizard保存とrollback

1. 1つのSet成功を実装する。
2. 複数操作の順序を固定する。
3. 途中失敗のrollbackを一ケースずつ実装する。
4. Config保存失敗のrollbackを実装する。
5. rollback失敗の復旧案内を実装する。

### Step 7: 旧形式移行

1. legacyなしのno-opから始める。
2. 1つの未登録secret移行を実装する。
3. 既存Key優先、複数Key、空値を追加する。
4. Keychain失敗、Config失敗、rollbackを実装する。
5. 冪等性を確認する。
6. applicationとwizardの共通入口へ接続する。

### Step 8: CLI・統合テスト

1. config / runへSecretStoreを注入する。
2. help / version / whereの非アクセスを確認する。
3. 一時Configとfake Storeで移行から起動まで確認する。
4. stdout / stderr / error / JSONのsecret非包含を横断確認する。

### Step 9: OS別スモークとcross build

1. macOS Keychainでダミー値のSet / Get / Deleteを確認する。
2. Linux / Windowsは実行環境またはCIを用意して確認する。
3. headless Linuxのエラーとenv-only起動を確認する。
4. `make cross` とCGO無効buildを実行する。

### Step 10: リファクタリングと仕様同期

1. secret識別子、error、変換処理を一箇所へ集約する。
2. Config、migration、wizard、applicationの責務を再確認する。
3. `SPEC.md` とREADMEの保存説明を実装結果へ同期する。
4. テストリストをすべて完了させる。

各Stepでもテストは必ず一件ずつ red → green → refactor で進める。

## 実装上の注意

- secretを `fmt.Errorf` の引数や構造体dumpへ含めない。
- Config全体を `%+v` でログ出力しない。
- テストのダミーsecretも失敗メッセージへ不用意に出さない。
- OS Keychainのpermission promptを自動テストで発生させない。
- 環境変数を扱うテストへ `t.Parallel()` を付けない。
- legacy JSONを削除・上書きする前に全Keyの保存成功を確認する。
- rollbackはbest effortだが、失敗を黙殺しない。
- Keychainがない環境でも平文保存機能を追加しない。
- dependency追加後も単一バイナリ・CGO不要要件を検証する。

## 完了条件

- `config.json` にsecret名と値が保存されない。
- macOS / Windows / Linux系のOS資格情報ストアadapterがbuildできる。
- runtime secretが環境変数 → Keychainの順で解決される。
- env-only実行はKeychain未提供環境でも可能である。
- wizardがKeychainへ保存・維持・削除できる。
- wizard途中失敗時に可能な範囲で元状態へ戻る。
- 旧形式JSONが安全かつ冪等に移行される。
- 移行失敗時に旧JSONを変更しない。
- 平文fallbackが存在しない。
- stdout、stderr、error、JSON、dataへsecretを漏らさない。
- 単体・統合テストが実OS Keychain、network、利用者homeへ依存しない。
- 変更したGoファイルが `gofmt` 済みである。
- 以下がすべて成功する。

```bash
go test ./internal/secretstore ./internal/core ./cmd
go test ./...
go test -race ./...
go vet ./...
go build ./...
make cross
```

## 想定される変更

- `internal/secretstore/store.go`: Store interface、Key、ErrNotFound
- `internal/secretstore/keyring.go`: OS Keyring adapter
- `internal/secretstore/*_test.go`: adapter contractとerror変換
- `internal/core/config.go`: JSON DTO、LegacySecrets、secret除外
- `internal/core/config_test.go`: 新旧形式、secret非保存、permission
- `cmd/secrets.go`: runtime resolverとmigration（必要に応じて新設）
- `cmd/secrets_test.go`: env優先、backend障害、migration
- `cmd/application.go`: runtime resolverの配線
- `cmd/application_test.go`: SecretStoreを含むcomposition
- `cmd/wizard.go`: secretEdit、Keychain保存、rollback
- `cmd/wizard_test.go`: keep / set / delete / rollback
- `cmd/root.go`: 本番SecretStoreの注入
- `go.mod` / `go.sum`: keyring依存
- `SPEC.md` / README: 実装結果との同期

## 手動スモークテスト

実API keyは使わず、必ずダミー値で確認する。

1. `reporepo config` でダミーsecretを設定する。
2. OSの資格情報管理UIでservice=`reporepo`の項目を確認する。
3. `config.json` にsecret名・値がないことを確認する。
4. 環境変数を設定し、Keychain値より優先されることをfake API境界で確認する。
5. `reporepo config` で `-` を入力して削除する。
6. OSの資格情報管理UIから項目が消えたことを確認する。
7. テスト用項目をすべて削除する。
