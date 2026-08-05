# Plan: ウィザードpromptのRenderer移行とデッドコード除去

Status: draft

## 目的

コードレビューの指摘2件を解消する。

1. 設定ウィザードのprompt（`ReadLine` / `ReadSecret`）が `presentation.Renderer` を経由せず `fmt.Fprint` で直接出力している。SPEC 2.8の「各メッセージはsemantic roleを経由して生成し、ANSIを直接組み立てない」に反するため、`Renderer.Prompt` 経由へ移行する。
2. 未使用のデッドコード `ErrNotImplemented` を削除する。

## 前提

- SPEC 2.8を正とする。promptはdecorated TTYで装飾、plain（非TTY / `NO_COLOR` / `TERM=dumb`）でANSIなしのテキストにする。
- `internal/presentation/renderer.go` の `Renderer.Prompt` は既にdecorated/plainを判定して出力するため、変更不要。
- secret入力のecho抑止（TTYでは `term.ReadPassword`、非TTYでは行入力）を維持する。
- 既存のwizard挙動（keep / set / delete / rollback、secret非漏洩）を退行させない。

## スコープ

### 対象

- `cmd/wizard.go`: prompt出力を `Renderer.Prompt` 経由にする
- `cmd/wizard_test.go`: 既存呼び出しの修正＋decorated/plain promptのテスト追加
- `internal/clients/ai.go`: `ErrNotImplemented` の削除

### 対象外

- `Renderer.Prompt` の実装変更（現状のままで契約を満たす）
- その他のCLI出力（Title / Section / Summary / Warning等）の変更
- TUIのstyle変更
- wizardの入力フロー・保存フローの変更

## 設計

### 変更1: prompt出力のRenderer移行

`consoleWizardIO` にprompt書き込み関数を追加し、`ReadLine` / `ReadSecret` がそれを利用する。

```go
type consoleWizardIO struct {
	reader       *bufio.Reader
	in           io.Reader
	out          io.Writer
	writePrompt  func(string) error  // 追加
	isTerminal   func(int) bool
	readPassword func(int) ([]byte, error)
}

func newConsoleWizardIO(in io.Reader, out io.Writer, writePrompt func(string) error) wizardIO {
	if writePrompt == nil {
		writePrompt = func(s string) error {
			_, err := fmt.Fprint(out, s)
			return err
		}
	}
	return &consoleWizardIO{reader: bufio.NewReader(in), in: in, out: out,
		writePrompt: writePrompt, isTerminal: term.IsTerminal, readPassword: term.ReadPassword}
}
```

`ReadLine` / `ReadSecret` の `fmt.Fprint(c.out, prompt)` を `c.writePrompt(prompt)` へ置き換える。

配線元 `runConfigWizardStreams` で `factory(out)` のrendererを作り、その `Prompt` を渡す。

```go
func runConfigWizardStreams(in io.Reader, out, errOut io.Writer, factory presenterFactory, load, save, secrets) error {
	renderer := factory(out)
	return runConfigWizardWith(wizardDependencies{
		io: newConsoleWizardIO(in, out, renderer.Prompt), out: renderer, errOut: factory(errOut), ...
	})
}
```

nilフォールバックを用意するため、テストの既存2箇所（`wizard_test.go:17`, `:37`）は `nil` を渡して従来のplain出力を維持できる。

### 変更2: デッドコード削除

`internal/clients/ai.go` の `ErrNotImplemented` 宣言（とそのコメント）を削除する。他パッケージから参照はなく、`errors` importは `parseAnalysis` 等で引き続き使用するため維持する。

## テストリスト

### A. wizard promptのRenderer経由

- [ ] decoratedなRendererで `ReadLine` のpromptにANSI装飾が含まれる
- [ ] decoratedなRendererで `ReadSecret` のpromptにANSI装飾が含まれる
- [ ] plainなRendererでpromptにANSIが含まれない
- [ ] promptのlabelテキストが欠落しない
- [ ] prompt描画errorが呼び出し元へ返る
- [ ] 非TTY入力で従来どおり行入力できる（回帰）
- [ ] TTY secret入力のecho抑止を維持する（回帰）
- [ ] runConfigWizard経由の既存テストが全て通る（回帰）

### B. ErrNotImplemented削除

- [ ] `ErrNotImplemented` がコード中に存在しない
- [ ] clientsパッケージのテストが全て通る（回帰）

## 実装順序

### Step 1: prompt rendererテスト（red）

decorated / plainそれぞれで `newConsoleWizardIO` + `Renderer.Prompt` を使い、prompt出力を検証するテストを書いて失敗を確認する。

### Step 2: 実装（green）

`consoleWizardIO` へ `writePrompt` を追加し、`ReadLine` / `ReadSecret` を差し替え、`runConfigWizardStreams` で `renderer.Prompt` を配線する。既存テストの呼び出し箇所を `nil` で更新する。

### Step 3: デッドコード削除

`ErrNotImplemented` を削除し、clientsパッケージのコンパイルとテストを確認する。

### Step 4: 検証

以下を実行して完了条件を確認する。

```bash
gofmt -l .            # 空であること
go test ./...
go test -race ./...
go vet ./...
```

## 完了条件

- wizardのpromptがdecorated TTYで装飾、plainでANSIなしのテキストになる。
- wizardの入力・保存・rollback・secret非漏洩の既存挙動が全て維持される。
- `ErrNotImplemented` が削除されている。
- `gofmt` / `go test ./...` / `go test -race ./...` / `go vet ./...` が全て成功する。

## 想定される変更

- `cmd/wizard.go`: `writePrompt` フィールド追加、`ReadLine` / `ReadSecret` の差し替え、`runConfigWizardStreams` の配線
- `cmd/wizard_test.go`: 既存呼び出しの更新＋decorated/plain promptテスト追加
- `internal/clients/ai.go`: `ErrNotImplemented` 削除

## worktree

- branch: `fix/wizard-prompt-presentation`
- worktree path: `/Users/issy20/ccplayground/reporepo/wizard-prompt-presentation`

理由: 2件とも今回のレビュー指摘の修正であり、実質的な変更はwizard promptのRenderer移行が主体。デッドコード削除は同一の小規模修正として同worktreeに含める。
