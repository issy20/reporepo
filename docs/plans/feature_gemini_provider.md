# Plan: Gemini AI provider対応

Status: implemented (自動検証完了、実キーによる手動スモークテスト未実施)

## 目的

既存のClaude／OpenAIに加えて、Google Geminiをリポジトリ解析のAI providerとして利用できるようにする。

利用者は `GEMINI_API_KEY` またはOS資格情報ストアへ保存したAPI keyを使い、設定ウィザードやTUIから `gemini` を選択できる。Gemini固有処理は既存の `clients.AIClient` 境界へ閉じ込め、Claude／OpenAIの挙動を変えない。

## 前提と技術選定

- 初期対象はGemini Developer APIとする。
- Vertex AI、Application Default Credentials、Google Cloud project/location設定は対象外とする。
- 公式Go SDK `google.golang.org/genai` を利用する。
- SDKは `internal/clients` のGemini adapter内だけで利用する。
- API keyはSDKの暗黙の環境変数解決へ任せず、既存のsecret resolverで明示的に解決して渡す。
- provider IDは `gemini` とする。
- 環境変数は `GEMINI_API_KEY` とする。
- Keychain accountは `gemini-api-key` とする。
- 既定モデルは実装開始時にGoogle公式モデル一覧を再確認し、安定版のFlash系モデルを明示指定する。
- `latest`／preview／experimental aliasは既定値に使わない。

公式SDKは `Models.GenerateContent` と `GenerateContentResponse.Text()` を提供する。モデルは廃止・更新されるため、計画書に記載したモデル名を無検証で固定しない。

## スコープ

### 対象

- Gemini clientの実装
- `GEMINI_API_KEY` のruntime解決
- OS資格情報ストアでのGemini API key管理
- Configと旧形式JSON移行へのGemini field追加
- 設定ウィザードへのGemini追加
- application compositionへのGemini client追加
- TUIのprovider選択を2択から3択へ変更
- Gemini解析結果の履歴・キャッシュ利用
- in-memory SecretStoreとfake Gemini APIによる自動テスト
- SPEC、README、設定説明の更新

### 対象外

- Vertex AI backend
- Google Cloud IAM／ADC
- Geminiの画像・音声・動画入力
- streaming表示
- function calling、Google Search grounding、code execution
- モデル一覧をAPIから動的取得する機能
- provider間の自動リトライ
- 既存解析履歴の変換

## ユーザー向け仕様

### Provider

```text
claude
openai
gemini
```

### Secret解決順序

Gemini API keyは次の順で解決する。

1. 空白を除いた `GEMINI_API_KEY`
2. OS資格情報ストアのservice=`reporepo`、account=`gemini-api-key`
3. 未設定

環境変数が存在する場合はKeychainへアクセスしない。環境変数の値をKeychainや `config.json` へ書き戻さない。

### Provider選択

- 設定ウィザードで `gemini` を既定providerとして選択できる。
- TUIの `p` キーは、利用可能なproviderだけを決定的な順序で巡回する。
- 巡回順は `claude` → `openai` → `gemini` とする。
- API keyがないproviderはapplicationのAI mapへ登録しない。
- 保存された既定providerが利用不能な場合は、上記順序で最初の利用可能providerを選ぶ。
- 3 providerすべてが未設定なら、既存の起動エラーをGeminiを含む案内へ更新する。

### 設定ウィザード

- `Gemini API key` の入力を追加する。
- 空入力は既存値維持、`-` は削除、それ以外は新規保存とする。
- 値そのものは表示しない。
- summaryには「設定済み」「環境変数」「未設定」の状態だけを表示する。
- 保存途中の失敗ではGeminiを含む全secret操作を逆順rollbackする。

## アーキテクチャ

```text
cmd
├── config / secret resolution
├── Gemini client factory
└── map[provider]clients.AIClient
                  │
                  ▼
internal/tui ── clients.AIClient
                  ▲
                  │
internal/clients/GeminiClient
                  │
                  ▼
       Gemini API境界（注入可能）
                  │
                  ▼
        google.golang.org/genai
```

TUIはGemini SDKを知らず、既存の `AIClient.Generate` だけを利用する。

## Gemini client設計

### 公開constructor

既存factoryと揃え、applicationからAPI keyとmodelを渡す。

```go
func NewGeminiClient(apiKey, model string) (*GeminiClient, error)
```

SDK client生成がerrorを返すため、application側のfactoryもerrorを扱える形へ変更する。Claude／OpenAI factoryまで一律にerror化するか、Gemini factoryだけを分けるかは、最初のcompositionテストで最小の変更を選ぶ。

### SDK境界

単体テストでnetworkを使わないよう、GeminiClientは必要最小限のinterfaceへ依存する。

```go
type geminiGenerator interface {
    GenerateContent(
        context.Context,
        string,
        []*genai.Content,
        *genai.GenerateContentConfig,
    ) (*genai.GenerateContentResponse, error)
}
```

SDKの実際のmethod signatureは依存version確定時にcompileで確認し、このinterfaceを正確に合わせる。SDK型への依存がテストを複雑にする場合は、adapter内にさらに小さい独自request／response境界を置く。

### Promptとresponse

- `buildPrompts` を再利用する。
- system promptはSDKのsystem instruction設定へ渡す。
- repository情報とREADMEをuser contentとして渡す。
- JSON出力を要求できる安定APIがSDKにあれば `application/json` を指定する。
- response textを `parseAnalysis(raw, language, "gemini", model)` へ渡す。
- candidateなし、textなし、安全性によるblock、SDK errorを区別して安全なerrorへ変換する。
- API keyやresponse body全体をerrorへ含めない。
- `context.Context` のcancel／deadlineを維持する。

## ConfigとSecretStore

### Config

`core.Config` にruntime専用fieldを追加する。

```go
GeminiAPIKey string `json:"-"`
```

旧形式読み込み用 `configFile` と `LegacySecrets` には `gemini_api_key` を追加する。`SaveConfig` は引き続きproviderと言語だけを保存する。

### SecretStore

```go
const GeminiAPIKey Key = "gemini-api-key"
```

- `Key.valid()` の許可対象へ追加する。
- Keyring adapterのserviceは引き続き `reporepo` とする。
- in-memory StoreはKeyを汎用mapで扱うため、本体変更なしでGemini keyを保持できることを確認する。

### Legacy migration

- 旧JSONの `gemini_api_key` をmigration対象に加える。
- 既存Keychain値を上書きしない。
- 4つ目のsecret操作として既存transaction／rollbackへ参加させる。
- 移行後のJSONへGeminiのfield名・値を含めない。

## Application composition

- `defaultGeminiModel` を追加する。
- `applicationDependencies` にGemini factoryを追加する。
- runtime ConfigにGemini keyがあれば `ai["gemini"]` を生成する。
- client初期化失敗はAPI keyやSDK内部情報を含まない起動エラーへ変換する。
- AI mapのcapacityを3へ変更する。
- 同じcontext timeout方針を既存providerと共有する。

## TUI変更

現在の固定2値 `toggle` を、利用可能provider一覧を巡回する処理へ置き換える。

```go
func nextProvider(current string, available []string) string
```

provider一覧はmap iterationへ依存せず、`claude`、`openai`、`gemini` の固定優先順からAI mapに存在するものだけを抽出する。

- 初期providerが利用可能なら維持する。
- 利用不能なら最初の利用可能providerへ補正する。
- 1 providerだけなら `p` で変化しない。
- Gemini解析の `Analysis.Provider` は `gemini` になる。
- cache keyが既にprovider／modelを含むことを確認し、Claude／OpenAI結果と混同しない。

## TDDテストリスト

実装時は一度に一件だけ選び、red → green → refactorを繰り返す。

### A. SecretStoreとConfig

- [ ] `GeminiAPIKey` が有効なSecretStore Keyである
- [ ] Keyring adapterがaccount=`gemini-api-key`を渡す
- [ ] ConfigをmarshalしてもGemini secret名と値を含まない
- [ ] SaveConfigがGemini API keyを保存しない
- [ ] 旧JSONからGemini API keyをLegacySecretsへ分離する
- [ ] 新形式Config読み込みでGemini API keyが空になる
- [ ] MemorySecretStoreでGemini keyをSet／Get／Deleteできる

### B. Runtime secret解決

- [ ] `GEMINI_API_KEY` がKeychain値より優先される
- [ ] envがあればGemini KeyをGetしない
- [ ] envがなければin-memory Storeから解決する
- [ ] 未登録ならGeminiを利用不能として扱う
- [ ] backend障害をsecret非包含のwarningへ変換する
- [ ] Geminiだけ設定済みならdefault providerをGeminiへ補正する
- [ ] 保存済みGemini providerとkeyがあれば維持する
- [ ] 全provider未設定のerrorがGemini設定方法も案内する

### C. Legacy migration

- [ ] 未登録のGemini legacy keyを移行する
- [ ] 既存Gemini Keychain値を上書きしない
- [ ] Geminiを含む移行が冪等である
- [ ] Gemini Set失敗でそれ以前の作成済みKeyをrollbackする
- [ ] Config保存失敗でGeminiを含む作成済みKeyをrollbackする
- [ ] 移行後JSONにGemini secretを含めない

### D. Gemini client

- [ ] GeminiClientが `clients.AIClient` を満たす
- [ ] nil metadataをAPI呼び出し前に拒否する
- [ ] unsupported languageをAPI呼び出し前に拒否する
- [ ] system／user promptとmodelをgeneratorへ渡す
- [ ] JSON responseを `core.Analysis` へ変換する
- [ ] Provider=`gemini`、Model=指定modelを記録する
- [ ] fenced JSON responseを解析できる
- [ ] candidate／textなしを安全なerrorにする
- [ ] safety blockを安全なerrorにする
- [ ] generator errorからAPI keyを除去する
- [ ] context cancel／deadlineを維持する
- [ ] 単体テストでnetworkへ接続しない

### E. Wizard

- [ ] Gemini API key promptを表示する
- [ ] 空入力で既存Gemini keyを維持する
- [ ] 入力値をGemini keyへ保存する
- [ ] `-` でGemini keyを削除する
- [ ] Geminiを既定providerとして選択できる
- [ ] Gemini envだけでもGeminiを選択できる
- [ ] Gemini key未設定時にGemini選択を確定しない
- [ ] summaryへGeminiの状態だけを表示する
- [ ] cancel／EOFでGemini keyを変更しない
- [ ] 4 secretの途中失敗を逆順rollbackする
- [ ] stdout／stderr／errorへGemini keyを含めない

### F. Application composition

- [ ] Gemini keyがあればGemini factoryを一度呼ぶ
- [ ] Gemini keyがなければGemini factoryを呼ばない
- [ ] GeminiだけでTUIを起動できる
- [ ] 3 provider設定時にAI mapへ3 clientを渡す
- [ ] Gemini client初期化失敗時にTUIを起動しない
- [ ] 初期化errorへAPI keyを含めない
- [ ] Claude／OpenAIだけの既存起動が変わらない

### G. TUI provider選択

- [ ] Geminiを初期providerとして受け入れる
- [ ] 3 providerを固定順で巡回する
- [ ] 利用不能providerを巡回対象から除外する
- [ ] 1 providerだけなら切り替わらない
- [ ] 未知providerを最初の利用可能providerへ補正する
- [ ] Gemini clientへ解析commandを送れる
- [ ] Geminiの履歴をprovider／model込みで保存する
- [ ] Gemini cacheを他providerの結果と混同しない

### H. 回帰とセキュリティ

- [ ] Claude clientの既存テストが成功する
- [ ] OpenAI clientの既存テストが成功する
- [ ] config wizardの既存入力フローを意図どおり更新する
- [ ] secretをconfig.json／data.jsonへ含めない
- [ ] 自動テストで実KeychainとGemini APIへ接続しない
- [ ] race testとCGO無効buildが成功する

## 実装順序

### Step 1: 仕様と契約

1. `SPEC.md` へGemini provider、secret、選択規則を追加する。
2. provider ID、環境変数、Keychain accountを確定する。
3. 実装時点のGoogle公式資料でSDK、安定モデル、GenerateContent仕様を確認する。

### Step 2: SecretStoreとConfig

1. `GeminiAPIKey` Keyのredテストを書く。
2. Configのsecret非保存テストを追加する。
3. LegacySecretsとmigrationを一件ずつ拡張する。
4. rollbackと冪等性を確認する。

### Step 3: Runtime resolver

1. env優先のredテストを書く。
2. in-memory Storeからの解決を追加する。
3. provider補正を2択の分岐から順序付き一覧へ整理する。
4. backend warningと全未設定errorを更新する。

### Step 4: Gemini client

1. 公式SDK依存を追加する。
2. 注入可能なgenerator境界を作る。
3. prompt送信のredテストから最小実装する。
4. response parsing、空response、block、error redactionを追加する。
5. 共通prompt／parse処理との重複を整理する。

### Step 5: Application composition

1. Gemini factory呼び出しのredテストを書く。
2. runtime ConfigからGemini clientを生成する。
3. constructor errorの安全な変換を追加する。
4. 既存2 providerの起動回帰を確認する。

### Step 6: Wizard

1. Gemini promptを追加する。
2. keep／set／deleteを追加する。
3. provider選択とsummaryを3 provider対応にする。
4. rollback順序とsecret非漏洩を確認する。

### Step 7: TUI

1. Gemini初期providerのredテストを書く。
2. 固定2値toggleを利用可能provider巡回へ置き換える。
3. Gemini解析command、履歴、cacheを確認する。
4. view helpとprovider表示の回帰を確認する。

### Step 8: 統合と文書

1. Geminiだけを設定したapplication統合テストを追加する。
2. 全テスト、race、vet、buildを実行する。
3. READMEへ設定例とKeychain保存先を追記する。
4. 公式モデル名と依存versionを再確認する。

## 完了条件

- `GEMINI_API_KEY` またはOS資格情報ストアからGemini keyを解決できる。
- `config.json` と `data.json` にGemini API keyを保存しない。
- `reporepo config` でGemini keyと既定providerを設定できる。
- Geminiだけを設定した状態でTUIを起動し、解析できる。
- TUIで利用可能なClaude／OpenAI／Geminiを切り替えられる。
- Gemini解析のproviderとmodelが履歴・cacheへ正しく反映される。
- API error、CLI出力、テスト失敗出力へAPI keyが漏れない。
- 自動テストが実Gemini APIと実OS資格情報ストアへ接続しない。
- 既存Claude／OpenAI機能が回帰しない。
- 次のコマンドがすべて成功する。

```bash
gofmt -w internal/clients cmd internal/core internal/secretstore internal/tui
go test ./internal/clients ./internal/core ./internal/secretstore ./internal/tui ./cmd
go test ./...
go test -race ./...
go vet ./...
CGO_ENABLED=0 go build ./...
make cross
```

## 想定される変更

- `internal/clients/gemini.go`: Gemini AIClient adapter
- `internal/clients/gemini_test.go`: fake generatorによる単体テスト
- `internal/core/config.go`: Gemini runtime／legacy field
- `internal/core/config_test.go`: secret非保存とlegacy読み込み
- `internal/secretstore/store.go`: Gemini Keychain account
- `internal/secretstore/keyring_test.go`: account変換テスト
- `internal/testutil/secretstore_test.go`: Gemini keyの契約確認
- `cmd/secrets.go`: env／Keychain解決とprovider補正
- `cmd/secrets_test.go`: in-memory resolver／migrationテスト
- `cmd/wizard.go`: Gemini key編集とprovider選択
- `cmd/wizard_test.go`: wizard／rollbackテスト
- `cmd/application.go`: Gemini factoryとAI map登録
- `cmd/application_test.go`: compositionテスト
- `internal/tui/model.go`: Gemini初期provider対応
- `internal/tui/update.go`: 利用可能provider巡回
- `internal/tui/*_test.go`: 選択・解析・履歴・表示テスト
- `go.mod` / `go.sum`: `google.golang.org/genai`
- `SPEC.md` / `README.md`: ユーザー向け仕様と設定方法

## 手動スモークテスト

実キーを扱うため、自動テスト完了後に必要最小限で実施する。

1. `GEMINI_API_KEY` だけを設定して起動する。
2. 公開repositoryをGeminiで解析する。
3. 日本語／英語のJSON解析結果を確認する。
4. `reporepo config` でKeychainへGemini keyを保存する。
5. 環境変数なしでKeychain値から起動する。
6. Claude／OpenAI／GeminiをTUIで切り替える。
7. Gemini keyを削除し、資格情報管理UIから消えたことを確認する。

実キー、HTTP header、raw error responseは記録・貼付しない。
