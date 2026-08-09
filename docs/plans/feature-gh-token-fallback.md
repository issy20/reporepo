# Plan: GitHub token の `gh auth token` フォールバック

Status: draft

## 目的

GitHub token を手動で用意して `GITHUB_TOKEN` や資格情報ストアへ設定する手間を省く。`gh` CLI が認証済みなら、環境変数・資格情報ストアの両方に GitHub token が無い場合に限り `gh auth token` の出力を実行時 Config の GitHub token として借用する（SPEC 2.7 の secret 解決順序 3）。

## 前提

- secret 解決順序は「環境変数 > OS資格情報ストア >（GitHub tokenのみ）`gh auth token` > 未設定」を維持する。
- フォールバックは GitHub token 専用。Anthropic / OpenAI / Gemini の API key には適用しない。
- `gh auth token` の取得失敗は未設定として扱い、エラーにしない。
- 取得したトークンは実行時のメモリ内のみで保持し、資格情報ストア・設定JSON・ログ・stdout/stderr へ書き込まない。
- `buildRuntime` の呼び出し元は `runApplicationWith` と `runAnalyze` の2箇所のみ（現状と同じ）。

## スコープ

### 対象

- `cmd/application.go`: `applicationDependencies` に `ghAuthToken func() (string, error)` を追加し、既定実装を `ghCLIToken` にする。`buildRuntime` で GitHub token 未設定時に呼び出す。
- `cmd/secrets.go`: `ghCLIToken` を追加（`gh auth token` を実行し、出力を trim して返す）。
- `cmd/application_test.go`: フォールバックのテストを追加。
- `cmd/secrets_test.go`: `ghCLIToken` のテストを追加（フェイク `gh` を PATH に配置）。
- `README.md`: 設定セクションにフォールバックの説明を追加。

### 対象外

- AI provider（Anthropic / OpenAI / Gemini）の secret フォールバック。
- `reporepo config` ウィザードの変更（gh トークンを保存対象にしない）。
- 未認証実行のままの既存挙動（gh が無い・認証されていない環境は従来どおり動作）。

## 設計

### 変更1: `ghCLIToken`（secrets.go）

`gh auth token` を固定コマンドとして実行し、標準出力を trim して返す。失敗時はエラーを返し、呼び出し側は未設定として扱う。

```go
func ghCLIToken() (string, error) {
	cmd := exec.Command("gh", "auth", "token")
	cmd.Stderr = io.Discard
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
```

### 変更2: 依存の注入とフォールバック呼び出し（application.go）

`applicationDependencies` に `ghAuthToken func() (string, error)` を追加する。`defaultApplicationDependencies()` が既定実装 `ghCLIToken` を設定する。`buildRuntime` では nil フォールバックを**行わない**（テストで実 `gh` が実行されないようにするため。production の全経路は `defaultApplicationDependencies()` 経由で注入済み）。

`buildRuntime` の `resolveRuntimeSecrets` 成功後、GitHub token が空のときだけ呼び出す。

```go
runtimeConfig, warnings, err := resolveRuntimeSecrets(cfg, deps.secretStore)
for _, warning := range warnings {
	warn(warning)
}
if err != nil {
	return nil, err
}
if runtimeConfig.GithubToken == "" && deps.ghAuthToken != nil {
	if token, ghErr := deps.ghAuthToken(); ghErr == nil && strings.TrimSpace(token) != "" {
		runtimeConfig.GithubToken = strings.TrimSpace(token)
	}
}
```

## テストリスト

### A. buildRuntime のフォールバック（application_test.go）

- [ ] 環境変数・資格情報ストアの両方に GitHub token が無いとき、`ghAuthToken` が呼ばれ、その出力が GitHub token として使われる
- [ ] 環境変数に GitHub token があれば `ghAuthToken` を呼ばない（環境変数優先）
- [ ] 資格情報ストアに GitHub token があれば `ghAuthToken` を呼ばない
- [ ] `ghAuthToken` がエラーを返したら未設定として扱い、エラーにしない
- [ ] `ghAuthToken` の出力が trim されて使用される
- [ ] `gh` から取得した token を資格情報ストアへ書き戻さない
- [ ] フォールバックは GitHub token 専用（AI key が無ければ従来どおりエラーになり、`ghAuthToken` は呼ばれない）
- [ ] runApplicationWith（TUI 経路）でもフォールバックが機能する

### B. ghCLIToken（secrets_test.go）

- [ ] PATH に置いたフェイク `gh` が `auth token` でトークンを出力すると、その値を返す
- [ ] フェイク `gh` がエラー終了するとエラーを返す

### C. 回帰

- [ ] 既存テスト（application / analyze / secrets）が全て通る
- [ ] 実環境（gh なし）でも従来どおり動作する（手動スモーク）

## 実装順序

### Step 1: テスト（red）

- `application_test.go`: A の各テストを追加して失敗を確認する。
- `secrets_test.go`: B のテストを追加して失敗を確認する（`ghCLIToken` 未実装のため）。

### Step 2: 実装（green）

- `secrets.go`: `ghCLIToken` を追加。
- `application.go`: `ghAuthToken` フィールドと既定実装、`buildRuntime` でのフォールバック呼び出しを追加。

### Step 3: 検証

```bash
gofmt -l .            # 空であること
go test ./...
go test -race ./...
go vet ./...
```

## 完了条件

- GitHub token 未設定かつ `gh` 認証済み環境で、明示設定なしに GitHub API が認証付きで呼ばれる。
- 環境変数・資格情報ストアの優先順位が変わらない。
- `gh` が無い・認証されていない環境では従来どおり動作する。
- `gofmt` / `go test ./...` / `go test -race ./...` / `go vet ./...` が全て成功する。

## 想定される変更

- `cmd/application.go`: `applicationDependencies.ghAuthToken` 追加、`defaultApplicationDependencies` の既定実装、`buildRuntime` のフォールバック呼び出し
- `cmd/secrets.go`: `ghCLIToken` 追加
- `cmd/application_test.go`: フォールバックのテスト追加
- `cmd/secrets_test.go`: `ghCLIToken` のテスト追加
- `README.md`: 設定セクションへフォールバック説明を追加

## worktree

- branch: `fix-analyze-output-hardening`
- worktree path: `/Users/issy20/ccplayground/reporepo/fix-analyze-output-hardening`

理由: 直前の analyze 出力修正と同じ worktree で作業中。GitHub token 解決の追加は `cmd` パッケージに収まる小規模変更のため、このまま継続する。
