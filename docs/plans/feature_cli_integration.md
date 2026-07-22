# Plan: CLI・設定・TUI統合起動フロー

Status: ready

## 背景

`main` には以下が統合済みである。

- TUIのモデル、解析フロー、キー操作、View、リサイズ対応
- Cobraのroot / run / config / version / where
- 設定ファイルと環境変数の読み込み
- 対話式設定ウィザード
- Store、GitHub、Claude、OpenAI、TUIの基本配線
- `main.go`からのCLI起動と終了コード反映

各レイヤーの単体テストは存在するが、CLIから設定・依存組み立て・TUI起動境界までを通した統合契約は十分に固定されていない。

現状には特に以下の課題がある。

- API keyが空でもClaude / OpenAI clientを両方生成する。
- 両AI keyが空でもTUIを起動する。
- 既定providerのkeyがなく、もう一方だけ利用可能な場合の補正がない。
- Config、パス、TUIの下位エラーを `%w` で露出し、secretや内部パスが漏れる可能性がある。
- `http.DefaultClient` を直接共有し、有限timeoutが明示されていない。
- `config`で保存した値を`run`が読む縦切りテストがない。
- 実バイナリのhelp / version / where / config / エラー終了コードを確認するテストがない。

## 目的

CLIを起点とした以下の実行経路を、安全かつ決定的に動作させる。

```text
reporepo config
  → 保存済みConfig
      → reporepo / reporepo run
          → 環境変数上書き
          → データパス解決
          → Store生成
          → GitHub / 利用可能なAI client生成
          → provider補正
          → TUI起動
```

また、次の非対話コマンドを実バイナリ境界で検証する。

```text
reporepo --help
reporepo version
reporepo where
```

## スコープ

### 対象

- `cmd/application.go`
- `cmd/application_test.go`
- `cmd/root.go`
- `cmd/root_test.go`
- `cmd/config_test.go`
- 必要に応じて `main_test.go`
- 必要に応じてCLI統合テスト用helper
- Makefileの安全なsmoke target

### 対象外

- 実API keyを使ったGitHub / Claude / OpenAI疎通
- API clientのHTTPリクエスト形式変更
- TUIの表示・キー操作変更
- Config wizardの質問項目追加
- GoReleaser・GitHub Actions・Homebrew
- モデル選択UI

通常テストではネットワークへ接続せず、実API keyも使用しない。

## 統合境界

### 1. CLI command境界

- root引数なしと`run`は同じapplication runnerを一度だけ呼ぶ。
- `config`は保存済み設定だけを読み、実行時環境変数の値を保存しない。
- `version`と`where`はConfig、HTTP client、TUIを初期化しない。
- `--help`はファイル読み書き、パス生成、TUI起動を行わない。
- 全commandは位置引数を仕様どおり拒否する。

### 2. Config境界

- `config`で保存したConfigを`LoadStoredConfig`がそのまま返す。
- `LoadConfig`は保存値のコピーへ環境変数を上書きする。
- 環境変数上書き後も設定ファイルの内容は変化しない。
- Config pathは`config`、`where`、`run`で同じresolverを利用する。
- テストでは`XDG_CONFIG_HOME`相当と一時ディレクトリを使用し、利用者の実設定へ触れない。

### 3. Application composition境界

application runnerは次の順で処理する。

1. 実行時Configを読み込む。
2. AI keyの利用可否を検証する。
3. 既定providerを利用可能なproviderへ補正する。
4. data pathを解決する。
5. timeout付きHTTP clientを一つ生成する。
6. Storeを生成する。
7. GitHub clientを生成する。
8. keyがあるAI clientだけを生成する。
9. `tui.Dependencies`と実行時ConfigをTUIへ渡す。
10. TUIを一度だけ起動する。

失敗した段階より後の処理は呼ばない。

### 4. TUI起動境界

- Store、GitHub、少なくとも1つのAI client、Nowを必ず渡す。
- GitHub tokenは空でも起動可能。
- AI mapには空keyのproviderを登録しない。
- TUIへ渡すConfigのDefaultProviderはAI mapに存在する。
- TUI終了時のnil errorをCLI成功として扱う。
- TUI起動エラーをCLI失敗として扱うが、生エラーを利用者へ露出しない。

## 設計方針

### 1. application依存の拡張

現在の `applicationDependencies` はConfig、data path、runTUIだけを注入する。統合テストで組み立て内容を観測できるよう、factoryを追加する。

例:

```go
type applicationDependencies struct {
    loadConfig   func() (*core.Config, error)
    dataPath     func() (string, error)
    newHTTP      func() *http.Client
    newStore     func(string) entryStore
    newGitHub    func(*http.Client, string, string) clients.GitHubClient
    newClaude    func(string, string, *http.Client) clients.AIClient
    newOpenAI    func(string, string, *http.Client) clients.AIClient
    runTUI       func(tui.Dependencies, *core.Config) error
}
```

実際の型境界に合わせて最小化し、テストのためだけに各clientの内部フィールドを公開しない。本番default依存を一箇所で補完し、テストごとに必要なfactoryだけを差し替えられるようにする。

### 2. API keyとprovider

| Anthropic | OpenAI | 実行時provider |
|---|---|---|
| なし | なし | エラー、TUI非起動 |
| あり | なし | `claude` |
| なし | あり | `openai` |
| あり | あり | 有効なDefaultProviderを維持 |

- 両方あり、DefaultProviderが不正なら`claude`へ補正する。
- provider補正は実行時Configのコピーに対して行う。
- 保存済みConfigを変更・保存し直さない。
- 空白だけのkeyは未設定として扱う。
- GitHub tokenは空白だけなら未設定として扱う。

### 3. HTTP client

- `http.DefaultClient`を直接使わない。
- 有限timeoutを持つclientをapplication起動ごとに一つ生成する。
- 同じポインタをGitHub / Claude / OpenAI factoryへ渡す。
- timeoutは定数化する。
- テストでは通信せず、渡されたclientとtimeoutだけを確認する。

具体的timeoutは実装時にプロジェクト方針を確定する。AI生成を不必要に短く切らず、無期限にも設定しない。

### 4. 安全なエラー変換

CLI境界で次の固定メッセージへ変換する。

- Config: `設定を読み込めませんでした`
- data path: `データ保存先を解決できませんでした`
- AI key不足: `ANTHROPIC_API_KEY または OPENAI_API_KEY を設定してください`
- TUI: `TUIを起動できませんでした`

- 下位エラーをそのまま `%w` で利用者向けerrorへ連結しない。
- テストで必要な原因判定は内部エラー型またはログではなく、呼び出し記録で行う。
- stdout、stderr、返却errorのすべてについてsecret非包含をテストする。
- `where`のpath解決エラーも生OSエラーを露出しない。

### 5. 実行時Configのコピー

`LoadConfig`から受け取ったポインタを直接補正せず、値コピーを作る。

```go
runtimeConfig := *loaded
```

これにより、provider補正がloader所有Configや保存済みConfigへ波及しないことをテスト可能にする。

### 6. プロセス境界テスト

Go test内でビルドしたバイナリまたはsubprocess helperを使い、次を確認する。

- 終了コード
- stdout / stderr
- 一時XDG配下のファイル有無・内容・permission
- 利用者homeへ書き込まないこと

実TUI起動は端末依存で停止し得るため、通常のsubprocessテストでは行わない。run成功経路は注入したfake TUIで検証し、実バイナリではAI key不足による安全な早期終了までを確認する。

### 7. 安全なsmoke target

Makefileへ必要に応じて次のようなtargetを追加する。

```make
smoke:
	go run . --help
	go run . version
	go run . where
```

`where`がファイルを作成しないことを前提とする。`run`や実API呼び出しは自動smokeへ含めない。

## TDDテストリスト

以下から常に一つだけ選び、具体的なテストを追加してredを確認してからプロダクトコードを変更する。

### A. Configの縦切り

- [ ] wizardで保存したConfigをLoadStoredConfigが返す
- [ ] LoadConfigが保存値へ環境変数を上書きする
- [ ] 環境変数上書き後も設定ファイル内容が変わらない
- [ ] GitHub token、Anthropic key、OpenAI keyの優先順位を個別に確認する
- [ ] config / where / runが同じConfig pathを使用する
- [ ] 一時設定ディレクトリ以外へ書き込まない
- [ ] Config fileが0600で保存される（対応OSのみ）

### B. AI keyとprovider補正

- [ ] 両AI keyなしではTUIを起動しない
- [ ] 両AI keyなしのエラーが環境変数名を案内する
- [ ] Anthropic keyだけならClaude clientだけを生成する
- [ ] Anthropic keyだけならproviderをclaudeへ補正する
- [ ] OpenAI keyだけならOpenAI clientだけを生成する
- [ ] OpenAI keyだけならproviderをopenaiへ補正する
- [ ] 両keyありで有効なDefaultProviderを維持する
- [ ] 両keyありで不正providerをclaudeへ補正する
- [ ] 空白だけのkeyでAI clientを生成しない
- [ ] provider補正がloaderから返されたConfigを変更しない
- [ ] provider補正が設定ファイルを書き換えない

### C. 依存関係の組み立て

- [ ] Configを一度だけ読み込む
- [ ] data pathを一度だけ解決する
- [ ] timeout付きHTTP clientを一度だけ生成する
- [ ] 同じHTTP clientをGitHubと全AI clientへ渡す
- [ ] 解決したdata pathでStoreを生成する
- [ ] GitHub base URLとtokenをGitHub clientへ渡す
- [ ] Claude keyとmodelをClaude factoryへ渡す
- [ ] OpenAI keyとmodelをOpenAI factoryへ渡す
- [ ] Store / GitHub / AI / NowをTUIへ渡す
- [ ] 補正済みConfigをTUIへ渡す
- [ ] TUIを一度だけ起動する
- [ ] GitHub tokenなしでもTUIを起動する

### D. 失敗時の停止順序

- [ ] Config失敗後にpath / factory / TUIを呼ばない
- [ ] Configがnilなら後続処理を呼ばない
- [ ] AI key不足後にpath / factory / TUIを呼ばない
- [ ] data path失敗後にfactory / TUIを呼ばない
- [ ] HTTP client factory失敗を扱う設計なら後続を呼ばない
- [ ] TUI失敗をCLIエラーにする
- [ ] TUI成功をnil errorとして返す

### E. secretと内部情報の保護

- [ ] Config loadの生エラーを返却errorへ含めない
- [ ] data pathの生エラーを返却errorへ含めない
- [ ] TUIの生エラーを返却errorへ含めない
- [ ] whereの生エラーをstdout / stderr / errorへ含めない
- [ ] GitHub tokenをstdout / stderr / errorへ含めない
- [ ] Anthropic keyをstdout / stderr / errorへ含めない
- [ ] OpenAI keyをstdout / stderr / errorへ含めない
- [ ] Config JSON全体をエラー表示しない

### F. command分離

- [ ] root引数なしとrunが同じrunnerを一度呼ぶ
- [ ] configがrun用Config loaderを呼ばない
- [ ] configがHTTP client / TUIを生成しない
- [ ] versionがConfig / path / HTTP / TUIを呼ばない
- [ ] whereがConfig内容 / HTTP / TUIを呼ばない
- [ ] helpがConfig / path / HTTP / TUIを呼ばない
- [ ] 全subcommandが余分な位置引数を拒否する
- [ ] 実行時エラーでusageを重複表示しない

### G. 実バイナリ・subprocess

- [ ] `reporepo --help`がexit 0でcommand一覧を表示する
- [ ] `reporepo version`がexit 0で一行表示する
- [ ] `reporepo where`がexit 0でconfig / data pathを表示する
- [ ] `where`実行だけでは設定・データファイルを作らない
- [ ] `reporepo config`へ入力をpipeして一時領域へ保存できる
- [ ] config出力に入力secretを含めない
- [ ] AI keyなしの`reporepo run`が非0で速やかに終了する
- [ ] AI key不足エラーにsecretや内部パスを含めない
- [ ] 利用者の実HOME / XDG配下を変更しない
- [ ] mainが成功時0、失敗時1を返す

### H. race・並列安全性

- [ ] 統合テストがグローバル環境変数を並列変更しない
- [ ] Config pathのテストseamを並列テストで共有しない
- [ ] `go test -race ./...`で競合がない
- [ ] subprocess終了後に一時ファイルやprocessを残さない

## 実装順序

### Step 1: 統合テスト用factoryを整える

1. `applicationDependencies`へ必要最小限のfactoryを追加する。
2. 本番default依存を生成する関数を分離する。
3. 既存applicationテストを維持する。
4. 呼び出し順・引数を記録できるfakeを用意する。

### Step 2: AI key検証とprovider補正

最初のTDDケース:

> Anthropic keyが空でOpenAI keyだけが設定され、DefaultProviderがclaudeの場合、OpenAI clientだけを生成し、providerをopenaiへ補正してTUIへ渡す。

1. 上記テストを追加してredを確認する。
2. 実行時Configのコピーを作る。
3. 利用可能AI mapを構築する。
4. 両keyなし、不正provider、空白keyを一件ずつ実装する。

### Step 3: HTTP clientと依存配線を固定する

1. HTTP clientの生成回数をテストする。
2. finite timeoutをテストする。
3. 各factoryへ同じclientが渡ることを確認する。
4. Store path、GitHub token、key、modelの受け渡しを一件ずつ固定する。

### Step 4: 失敗時の停止とエラー安全性

1. Config、key、path、TUIの順に失敗テストを追加する。
2. 各段階で後続呼び出しが0回であることを確認する。
3. secret文字列を含む下位エラーをfakeから返す。
4. stdout / stderr / errorにsecretがないことを一件ずつ確認する。
5. `where`のエラーも同じ方針へそろえる。

### Step 5: Config保存からrun読込までを統合する

1. 一時Config pathでwizard保存を実行する。
2. 同じpathからLoadStoredConfigする。
3. 環境変数を設定してLoadConfigの優先順位を確認する。
4. ファイル内容とpermissionが変化しないことを確認する。
5. 読み込んだConfigをfake TUIまで渡す。

### Step 6: command間の副作用分離

1. help / version / where / configごとに不要な依存が呼ばれないテストを追加する。
2. root / runの共通runnerを確認する。
3. 位置引数、usage、stderrの振る舞いを固定する。

### Step 7: 実バイナリ境界を検証する

1. 一時ディレクトリへバイナリをbuildする。
2. help / version / whereをsubprocessで実行する。
3. pipe入力でconfigを実行し、secret非表示と0600保存を確認する。
4. AI keyなしのrunが早期失敗することを確認する。
5. subprocessに専用HOME / XDG環境だけを渡す。

### Step 8: smoke targetと手動確認手順

1. 安全な非対話commandだけを実行するsmoke targetを追加する。
2. 実TUI確認は別の手動手順として文書化する。
3. 手動確認でも実設定を避け、一時XDGとテスト用keyを利用する。

### Step 9: リファクタリングと全体検証

1. composition、validation、error変換を責務ごとに整理する。
2. production factoryとtest fakeの境界を明確にする。
3. subprocess helperの重複を整理する。
4. テストリストをすべて完了させる。

各Stepでもテストは必ず一件ずつ red → green → refactor で進める。

## 手動スモークテスト

実装後、実APIへ接続しない範囲で次を確認する。

```bash
tmp_home="$(mktemp -d)"
HOME="$tmp_home" XDG_CONFIG_HOME="$tmp_home/config" XDG_DATA_HOME="$tmp_home/data" go run . --help
HOME="$tmp_home" XDG_CONFIG_HOME="$tmp_home/config" XDG_DATA_HOME="$tmp_home/data" go run . version
HOME="$tmp_home" XDG_CONFIG_HOME="$tmp_home/config" XDG_DATA_HOME="$tmp_home/data" go run . where
```

`config`はダミーsecretを使用し、終了後に一時ディレクトリを削除する。`run`の完全な実API疎通は、本フェーズの自動テストへ含めない。

## 実装上の注意

- 利用者の実HOME、Config、data.jsonへ触れない。
- テストへ実API keyを埋め込まない。
- subprocessへ親processのsecret環境変数を無条件に継承しない。
- 環境変数を扱うテストへ `t.Parallel()` を付けない。
- OS依存のpermissionテストはWindowsで適切にskipする。
- `http.DefaultClient`のglobal設定を変更しない。
- TUIを自動テストで対話起動してhangさせない。
- 実装済みの単体テストを統合テストへ置き換えず、両方維持する。
- モデルIDとAPI仕様の最新性確認は実API疎通フェーズで別途行う。

## 完了条件

- config保存値をrunが読み込み、環境変数を優先してTUI境界へ渡す。
- AI mapにはkey設定済みproviderだけが含まれる。
- 両AI key不足をTUI起動前に検出する。
- DefaultProviderが利用可能なproviderへ補正される。
- GitHub tokenなしでも起動境界まで進める。
- Config、path、HTTP client、Store、clients、TUIが正しい順序と回数で組み立てられる。
- help / version / where / configが不要な依存を初期化しない。
- stdout、stderr、errorにsecretや下位エラーを含めない。
- 実バイナリのhelp / version / where / config / key不足終了を一時環境で検証する。
- テストがnetwork、実terminal、利用者homeへ依存しない。
- 変更したGoファイルが`gofmt`済みである。
- 以下がすべて成功する。

```bash
go test ./cmd ./internal/core
go test ./...
go test -race ./...
go vet ./...
go build ./...
make smoke
```

## 想定される変更

- `cmd/application.go`: key検証、provider補正、factory、timeout、error変換
- `cmd/application_test.go`: composition、順序、provider、secret保護のテスト
- `cmd/root.go`: whereを含むCLIエラー安全性の補強
- `cmd/root_test.go`: command間の副作用分離テスト
- `cmd/config_test.go`: wizard保存からruntime読込までの統合テスト
- `main_test.go`: 終了コードまたはsubprocess境界テスト
- `Makefile`: 安全なsmoke target
- `docs/plans/feature_cli_integration.md`: TDD進捗の更新

## 後続フェーズ

1. 公式仕様を確認した上での実API疎通テスト
2. READMEのインストール・設定・利用方法
3. GoReleaserとversion ldflags
4. クロスコンパイル・リリースworkflow
