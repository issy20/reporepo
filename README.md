# Reporepo

GitHubリポジトリをAI（Claude / OpenAI / Google Gemini）で解析し、要約・技術解説・技術的背景を表示するTUIアプリです。

`owner/repo`（またはGitHub URL）を入力すると、GitHubからメタ情報・README・言語構成・依存マニフェストと主要ソースを取得し、選択中のAIプロバイダが構造化された解説（要約・技術スタック・技術的背景・キーワード）を生成します。生成結果はローカルに保存され、履歴・お気に入りから再閲覧できます。

## 機能

- **3つのAIプロバイダ**: Claude（Anthropic Messages API）、OpenAI（Chat Completions API）、Gemini（Gemini Developer API）
- **コード文脈を活用した解析**: READMEだけでなく依存マニフェスト（`go.mod` / `package.json` 等）と主要ソース（最大6ファイル・8000文字）をAI入力に追加し、技術スタックの解析精度を向上
- **多言語**: 日本語・英語を `l` キーで即時切替。結果は言語ごとに別々にキャッシュ
- **履歴とお気に入り**: 同じリポジトリは1エントリに集約。`tab` で履歴 ⇄ お気に入りを切替
- **キャッシュ優先**: 生成済みの結果はAIを再呼び出しせず即表示。`r` キーで強制再生成。AI入力定義が変わった古い解析は1回だけ自動再生成
- **キャッシュ鮮度の自動維持**: 開くたびに GitHub 由来のメタ情報（説明・スター数等）を 7 日間隔で自動再取得し、更新があれば反映（言語構成は維持）。古い解析は一覧の `◌` と詳細の案内で「リポジトリ更新前のもの」と分かるように表示。AI解析は費用がかかるため自動再生成せず、`r` キーでのみ再生成
- **疑似Trending一覧（学びのネタ探し）**: `t` キー（TUI）や `reporepo trending`（CLI）で「直近に作成されスターが伸びた」リポジトリの一覧を表示し、そのまま解析できる。GitHub Search APIで近似し、一覧は6時間キャッシュ
- **学習ノート**: 各リポジトリに自分のメモを保存・編集・表示（詳細画面で `n` → 編集 → `Ctrl+S` 保存）。解析の再生成・キャッシュ更新後もノートは保持
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
reporepo analyze owner/repo   # TUIなしで解析し結果を出力
reporepo trending # 直近に伸びたリポジトリの一覧を表示
```

`config` コマンドをスキップして環境変数だけで動かすこともできます。詳細は「設定」を参照してください。

## コマンド

| コマンド | 説明 |
|---|---|
| `reporepo run` | TUIを起動 |
| `reporepo analyze owner/repo` | リポジトリを解析して結果を出力（非対話） |
| `reporepo trending` | 直近に作成されスターが伸びたリポジトリの一覧を表示 |
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
| `t` | 疑似Trending一覧を表示 |
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
| `n` | 学習ノートを編集（`Ctrl+S`: 保存 / `Esc`: キャンセル） |
| `Esc` | 一覧へ戻る |

### Trending一覧画面

| キー | 操作 |
|---|---|
| `Enter` | 選択中のリポジトリを解析 |
| `↑` / `↓` | 一覧の選択を移動 |
| `t` | 一覧を再取得 |
| `Esc` | 入力画面へ戻る |

## 非対話解析（analyze コマンド）

TUIを起動せずにリポジトリを解析し、結果を stdout へ出力します。スクリプト・CI・パイプから利用できます。

```bash
reporepo analyze owner/repo
reporepo analyze --json owner/repo
reporepo analyze --provider gemini --language en owner/repo
reporepo analyze --force owner/repo
```

| フラグ | 既定 | 説明 |
|---|---|---|
| `--provider, -p` | `config.json` の `default_provider` | AI プロバイダ（claude / openai / gemini） |
| `--language, -l` | `config.json` の `default_language` | 出力言語（ja / en） |
| `--json` | false | 結果を単一の JSON オブジェクトとして出力 |
| `--force, -f` | false | キャッシュを無視して再生成 |

設定・secret・クライアント構築は `run` と同じ経路を使い、ストア（`data.json`）も共有します。解析結果は保存され、TUI の履歴にも現れます。キャッシュヒット時は AI を呼ばず保存済みを出力します（`--force` で再生成）。

出力は常に ANSI を含まない plain text で、`--json` 時は JSON オブジェクト1件です。警告・エラーは stderr、結果は stdout へ出力し、終了コードは成功 0 / 失敗 1 です。

## 疑似Trending一覧（trending コマンド）

GitHub には公式の Trending API が存在しないため、Search API で「直近に作成され、スターが伸びた」リポジトリを近似した一覧を表示します。AIキーが未設定でも動作します（GitHub API のみを使用）。

```bash
reporepo trending
reporepo trending --since week --language go
reporepo trending --min-stars 100 --json
```

| フラグ | 既定 | 説明 |
|---|---|---|
| `--since` | `week` | 作成日時ウィンドウ（`today` / `week` / `month`） |
| `--language` | なし | 言語絞り込み |
| `--min-stars` | `50` | スター数の下限 |
| `--json` | false | 結果を JSON 配列として出力 |

一覧は **6時間** キャッシュされ（`data.json` と同じディレクトリの `trending-cache.json`）、同じ条件の再取得を避けます。GitHub Search API のレート制限時は、キャッシュがあればそれを表示し、なければ時間をおいて再実行するよう案内します。

一覧で気になるリポジトリは `reporepo analyze owner/repo` に渡すことで、そのまま既存パイプラインで解析・保存できます（TUI では `t` キー → 選択 → Enter）。

## 学習ノート

各リポジトリのエントリに、自分の学習メモを保存できます。AI解析結果は参照用で、書き足したノートが学習の記録として蓄積されます。

- 詳細画面で `n` を押すと複数行エディタが開きます
- `Ctrl+S` で保存、`Esc` でキャンセル
- ノートはリポジトリ単位で `data.json` の `note` フィールドに保存され、解析の再生成（`r`）やキャッシュ更新後も保持されます
- ノートが空の場合は表示されません

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

GitHub tokenは任意です（未設定でも動作しますが、GitHub APIの低いレート制限が適用されます）。TUIの起動・Trending一覧・履歴・お気に入り・学習ノートはAI API keyなしでも利用できます。リポジトリの解析（`analyze` コマンド、またはTUIでEnter）にはAnthropic / OpenAI / GeminiのAPI keyのいずれか一つ以上が必要です。

#### GitHub tokenの自動借用

GitHub tokenが環境変数・資格情報ストアのどちらにも無い場合、`gh` CLIが認証済みなら `gh auth token` の出力を自動的に借用します（`gh` でのログインだけでGitHub APIを認証付きで呼べます）。このトークンは実行時のメモリ内だけで使い、資格情報ストアや設定JSONへ保存しません。

```bash
gh auth login        # 初回のみ
reporepo run
```

`gh` が無い・認証されていない環境では従来どおり未認証（低いレート制限）で動作します。

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

各解析結果にはAI入力のバージョン（`prompt_version`）が記録されます。この値が現在の入力定義と異なる古い解析はキャッシュ一致とみなされず、次に開いたときに1回だけ再生成されます。

キャッシュしたリポジトリのメタ情報は、前回の取得から **7 日** を超えて開いたときに `GET /repos/{owner}/{repo}` の 1 リクエストだけで自動再取得されます。リポジトリに更新があれば説明・スター数・トピック等を反映し（言語構成は維持）、なければ取得日時だけを更新します。再取得に失敗しても閲覧は中断せず、警告を表示します。詳細画面のヘッダに「取得: 3日前 / 解析: 5日前」のように取得日時と解析日時が表示され、解析結果がリポジトリの最終更新より古い場合は「この解析はリポジトリの更新前のものです（`r` で再生成）」という案内が表示されます。一覧画面では、古い解析を持つリポジトリに `◌` マークが付きます（一覧表示時に GitHub API は呼びません）。

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
  cmd/                         # Cobra CLI（run / analyze / trending / config / version / where）
    root.go                    #   ルートコマンド定義
    analyze.go                 #   analyze コマンド（非対話解析）
    analyze_output.go          #   analyze の plain / JSON 出力整形
    trending.go                #   trending コマンド（疑似Trending一覧）
    trending_output.go         #   trending の plain / JSON 出力整形
    wizard.go                  #   設定ウィザード
  internal/
    presentation/              # CLIのsemantic rendering（TTY / plain切替）
    analyzer/                  # 共有解析パイプライン（キャッシュ確認 → GitHub取得 → AI生成 → 保存）
    trendingcache/             # 疑似Trending一覧のファイルキャッシュ（TTL付き・アトミック保存）
    core/                      # データ型（Entry / RepoMeta / Analysis）と設定管理
    secretstore/               # OS資格情報ストア境界（keyring実装）
    store/                     # 解析履歴のJSON永続化
    clients/                   # 外部APIクライアント（GitHub / Claude / OpenAI / Gemini）
    tui/                       # Bubble Tea TUI（model / update / view / commands）
```

### リリース

`make cross` で単一バイナリをクロスコンパイルできます（CGO不要）。各OS向けのビルド済みバイナリは `dist/` に出力されます。

GoReleaser による GitHub Releases への自動リリースを `.github/workflows/release.yml` が担います。`v*` タグを push すると、`-ldflags` でバージョンが注入された各OS向けバイナリ・アーカイブ・`checksums.txt` が公開されます。

```bash
# ローカルでスナップショット検証（GitHub へは公開しない）
goreleaser release --snapshot --clean

# リリース（タグ push がトリガ）
git tag v0.1.0
git push origin v0.1.0
```

回帰テストは `.github/workflows/ci.yml` が PR と main への push で実行します（`go vet` / `go test` / `go test -race`）。
