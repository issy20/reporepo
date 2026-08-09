# Reporepo

GitHubリポジトリをAI（Claude / OpenAI / Google Gemini）で解析し、要約・技術解説・技術的背景を表示するTUIアプリです。

`owner/repo`（またはGitHub URL）を入力すると、GitHubからメタ情報・README・言語構成を取得し、選択中のAIプロバイダが構造化された解説（要約・技術スタック・技術的背景・キーワード）を生成します。生成結果はローカルに保存され、履歴・お気に入りから再閲覧できます。

## 機能

- **3つのAIプロバイダ**: Claude（Anthropic Messages API）、OpenAI（Chat Completions API）、Gemini（Gemini Developer API）
- **多言語**: 日本語・英語を `l` キーで即時切替。結果は言語ごとに別々にキャッシュ
- **履歴とお気に入り**: 同じリポジトリは1エントリに集約。`tab` で履歴 ⇄ お気に入りを切替
- **キャッシュ優先**: 生成済みの結果はAIを再呼び出しせず即表示。`r` キーで強制再生成
- **セキュアなsecret管理**: API keyは設定JSONへ保存せず、OSの資格情報ストアへ保存

## 要件

- Go 1.25 以上（ビルド・`go install`時）
- macOS: Keychain / Windows: Credential Manager / Linux・*BSD: Secret Service over D-Bus
  - LinuxでSecret Service（GNOME Keyring / KWallet / KeePassXC 等）が利用できない場合は、環境変数による実行ができます

## インストール

```bash
go install github.com/issy20/reporepo@latest
```

またはソースからビルド:

```bash
git clone https://github.com/issy20/reporepo.git
cd reporepo
make build
```

## クイックスタート

```bash
reporepo config   # 対話的にAPI keyを設定
reporepo run      # TUIを起動
```

`config` コマンドをスキップして環境変数だけで動かすこともできます。詳細は「設定」を参照してください。

## コマンド

| コマンド | 説明 |
|---|---|
| `reporepo run` | TUIを起動 |
| `reporepo config` | 設定を対話的に編集（設定ウィザード） |
| `reporepo version` | バージョンを表示 |
| `reporepo where` | 設定・データファイルの保存先を表示 |

## TUIの使い方

`run` を起動すると、リポジトリ入力欄と履歴・お気に入り一覧が表示されます。`owner/repo`、`https://github.com/owner/repo`、`git@github.com:owner/repo.git` のいずれの形式でも入力できます。

### 入力画面（一覧表示）

| キー | 操作 |
|---|---|
| `Enter` | 入力値、または選択中のリポジトリを解析 |
| `↑` / `↓` | リストの選択を移動 |
| `Tab` | 履歴 ⇄ お気に入りを切替 |
| `f` | 選択中のリポジトリをお気に入りに追加 / 解除 |
| `d` | 選択中の履歴を削除 |
| `l` | 言語（日本語 ⇄ 英語）を切替 |
| `p` | AIプロバイダを切替 |
| `q` / `Esc` | 終了 |

### 解析中

| キー | 操作 |
|---|---|
| `Esc` | 解析をキャンセル |

### 詳細画面（解析結果）

| キー | 操作 |
|---|---|
| `↑` / `↓` / `PgUp` / `PgDn` | 解説をスクロール |
| `l` | 言語を切替（その言語の結果が無ければ再生成） |
| `f` | お気に入りに追加 / 解除 |
| `r` | 選択中の言語で強制再生成 |
| `Esc` | 一覧へ戻る |

## 設定

### secretの保存

GitHub token、Anthropic API key、OpenAI API key、Gemini API keyは `config.json` へ保存せず、OSの資格情報ストアへ保存します（service=`reporepo`）。

| OS | 保存先 |
|---|---|
| macOS | Keychain |
| Windows | Credential Manager |
| Linux / *BSD | Secret Service over D-Bus |

LinuxではSecret Serviceを提供するkeyringとD-Busセッションが必要です。利用できない場合も平文ファイルへ自動的にフォールバックしません。

### 環境変数

実行時のsecret解決順序は次のとおりです。環境変数はOS資格情報ストアより優先され、その値が資格情報ストアやJSONへ書き戻されることはありません。

```bash
export GITHUB_TOKEN=...
export ANTHROPIC_API_KEY=...
export OPENAI_API_KEY=...
export GEMINI_API_KEY=...
reporepo run
```

GitHub tokenは任意です（未設定でも動作しますが、GitHub APIの低いレート制限が適用されます）。Anthropic / OpenAI / GeminiのAPI keyはいずれか一つ以上が必要です。

### 設定ウィザード

`reporepo config` では、secret・既定provider・既定言語を段階的に設定します。

- secretの空入力は**既存値の維持**、`-` は**削除**を意味します
- 既存secretの値は画面へ表示しません（設定済み / 未設定の状態のみ）
- 入力と保存はTTY上でechoせず、途中失敗時は更新前の状態へロールバックします

### 設定ファイル

`config.json`（`reporepo where` で確認できます）には非secret項目だけが保存されます。

```json
{
  "default_provider": "claude",
  "default_language": "ja"
}
```

### データファイル

解析結果は `data.json` へ保存されます。

- `$XDG_DATA_HOME/reporepo/data.json`（`XDG_DATA_HOME` 設定時）
- `~/.local/share/reporepo/data.json`（それ以外。全OS共通）

設定ファイルの保存先は `reporepo where` で確認できます。

### 旧形式からの移行

旧形式の `config.json` に平文secretが存在する場合、起動時または設定ウィザード開始時にOS資格情報ストアへ自動移行し、全項目の移行成功後にJSONからsecretを除去します。

## CLIプレゼンテーション

help、version、保存先、設定ウィザード、警告、エラーは共通の表示規則を使います。TTY では色と状態記号で装飾し、pipe・redirect、`NO_COLOR`、`TERM=dumb` では ANSI escape sequence を含まない plain text を出力します。

```bash
reporepo --help
reporepo version
reporepo where
NO_COLOR=1 reporepo --help
TERM=dumb reporepo config
```

plain mode の `version` は `reporepo 0.1.0` の1行、`where` は `config:` と `data:` の2行を維持するため、pipe からも利用できます。警告とエラーは stderr、それ以外の通常結果と prompt は stdout へ出力します。

## 開発

### 開発コマンド

```bash
make build    # バイナリをビルド
make test     # 全テストを実行
make lint     # go vet + gofmt 差分チェック
make vet      # go vet
make tidy     # 依存を整理
make run      # ローカル実行
make smoke    # API通信を伴わないCLIスモークテスト
make cross    # darwin/linux/windows × amd64/arm64 を一括ビルド
make clean    # 生成物を削除
```

```bash
go test ./...
go test -race ./...
go vet ./...
```

自動テストでは実OS資格情報ストアを読み書きしません。実backendの確認には必ずダミー値を使用してください。

### ディレクトリ構成

```
reporepo/
  main.go                      # エントリポイント
  cmd/                         # Cobra CLI（run / config / version / where）
    root.go                    #   ルートコマンド定義
    wizard.go                  #   設定ウィザード
  internal/
    presentation/              # CLIのsemantic rendering（TTY / plain切替）
    core/                      # データ型（Entry / RepoMeta / Analysis）と設定管理
    secretstore/               # OS資格情報ストア境界（keyring実装）
    store/                     # 解析履歴のJSON永続化
    clients/                   # 外部APIクライアント（GitHub / Claude / OpenAI / Gemini）
    tui/                       # Bubble Tea TUI（model / update / view / commands）
```

### リリース

`make cross` で単一バイナリをクロスコンパイルできます（CGO不要）。各OS向けのビルド済みバイナリは `dist/` に出力されます。
