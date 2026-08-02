# Reporepo

GitHubリポジトリをClaude、OpenAI、またはGoogle Geminiで解析し、要約・技術解説・技術的背景を表示するTUIアプリです。

## ビルドと実行

```bash
go build -o reporepo .
./reporepo config
./reporepo run
```

設定・データファイルの場所は `./reporepo where` で確認できます。

## secretの保存

GitHub token、Anthropic API key、OpenAI API key、Gemini API keyは `config.json` へ保存せず、OSの資格情報ストアへ保存します（service=`reporepo`、Gemini account=`gemini-api-key`）。

| OS | 保存先 |
|---|---|
| macOS | Keychain |
| Windows | Credential Manager |
| Linux / *BSD | Secret Service over D-Bus |

LinuxではSecret Serviceを提供するkeyringとD-Busセッションが必要です。利用できない場合も平文ファイルへ自動的にフォールバックしません。

環境変数はOS資格情報ストアより優先され、その値が資格情報ストアやJSONへ書き戻されることはありません。

```bash
export GITHUB_TOKEN=...
export ANTHROPIC_API_KEY=...
export OPENAI_API_KEY=...
export GEMINI_API_KEY=...
./reporepo run
```

GitHub tokenは任意です。Anthropic、OpenAI、GeminiのAPI keyはいずれか一つ以上が必要です。

設定ウィザードではsecretの空入力は既存値の維持、`-` は削除を意味します。既存secretの値は画面へ表示しません。

旧形式の `config.json` に平文secretが存在する場合、起動時または設定ウィザード開始時にOS資格情報ストアへ移行し、全項目の移行成功後にJSONからsecretを除去します。

## 開発時の検証

```bash
go test ./...
go test -race ./...
go vet ./...
make cross
```

自動テストでは実OS資格情報ストアを読み書きしません。実backendの確認には必ずダミー値を使用してください。
