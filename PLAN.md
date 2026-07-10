# Reporepo 実装計画（TDD ベース）

最終更新: 2026-07-07
対象仕様: [SPEC.md](./SPEC.md)
開発手法: t_wada 式テスト駆動開発（テストリスト → レッド → グリーン → リファクタリング）

---

## 0. 現状認識

- リポジトリには `SPEC.md` のみ存在し、**プロダクトコードは未着手**。git 未初期化。
- SPEC の「9.1 完了」は設計上の到達点であり、実体は本計画で**ゼロから実装**する。
- 各層の「振る舞い」を先にテストで固定し、実装で満たしていく。外部依存（GitHub / AI API）は
  `net/http/httptest` でモックし、ネットワークなしでテストを回せる状態を保つ。

### TDD の回し方（全フェーズ共通）

1. そのフェーズの**テストリスト**を書く（下記に用意済み。気づいたら追記する）。
2. リストから **1つだけ**選び、実行可能なテストに翻訳して**失敗**を確認（レッド）。
3. 最小限のプロダクトコードでテストを通す（グリーン）。過程で気づいた項目はテストリストへ追加。
4. 必要ならリファクタリング。
5. テストリストが空になるまで 2 へ戻る。

### 依存の向き（下から実装する）

```
core/types ─┬─> store ──┐
            └─> config  ├─> tui ─> cmd ─> main
        clients/github ─┤
        clients/ai ─────┤
   ├ clients/claude ────┤
   └ clients/openai ────┘
```

下位層（テストしやすい純粋ロジック）から積み上げ、TUI・CLI を最後に載せる。

---

## フェーズ 0: プロジェクト初期化

**ゴール:** `go build ./...` と `go test ./...` が空実装で通る土台を作る。

- [ ] `go mod init`（module path を仮の `github.com/yourname/reporepo` で作成。SPEC 9.2 の通り後で置換）
- [ ] ディレクトリ雛形を作成（`cmd/` `internal/core` `internal/store` `internal/clients` `internal/tui`）
- [ ] 依存を追加: `bubbletea` `bubbles` `glamour` `lipgloss` `cobra`
      → **バージョンは go.mod 固定値を鵜呑みにせず、`go mod tidy` で現行版に解決**（SPEC 9.2）
- [ ] `Makefile`（`build` / `test` / `lint` / `cross`）と `.gitignore` を用意
- [ ] `git init` して初期コミット

> このフェーズはテスト対象がないため TDD 対象外。ビルドが通ることだけ確認する。

---

## フェーズ 1: core（types / config）

**ゴール:** データ型の確定と、設定の読み書き・パス解決・環境変数優先を保証する。

### 1.1 types.go
- [ ] `Entry` / `RepoMeta` / `Analysis` を SPEC 5章通りに定義（JSON タグ付き）
- [ ] `Analyses` は `map[string]*Analysis`（言語キー）

型定義自体はテスト不要。JSON タグの往復は store のテストで担保する。

### 1.2 config.go — テストリスト
- [ ] `XDG_DATA_HOME` が設定済みなら `$XDG_DATA_HOME/reporepo/` を返す
- [ ] 未設定なら `~/.local/share/reporepo/` にフォールバックする
- [ ] 設定ファイルが存在しない場合、既定値の Config を返す（エラーにしない）
- [ ] 書き込み → 読み込みで内容が一致する（ラウンドトリップ）
- [ ] 設定ファイルのパーミッションが `0600` である（SPEC 3章）
- [ ] `GITHUB_TOKEN` 環境変数がファイル値より優先される
- [ ] `ANTHROPIC_API_KEY` が優先される
- [ ] `OPENAI_API_KEY` が優先される
- [ ] 既定モデル名を持つ（`claude-sonnet-4-6` / `gpt-4o-mini`）※実キー投入前に公式ドキュメントで再検証（SPEC 9.2）

> テストは `t.TempDir()` と `t.Setenv()` を使い、実ホームや実環境を汚さない。

---

## フェーズ 2: store（JSON 永続化）

**ゴール:** 「同一 repo を1エントリにまとめる」中核ロジックを厳密にテストで固定する。

### store.go — テストリスト
- [ ] 空ストア（ファイルなし）を読むと空スライスを返す
- [ ] 新規 Entry を保存して読み戻せる
- [ ] 同じ `full_name` を再保存すると**エントリ数は1のまま**（重複行にならない）
- [ ] 再保存時に `ViewedAt` が更新される（SPEC 2.4）
- [ ] `CreatedAt` は初回登録値を保持する（upsert で書き換えない）
- [ ] 1エントリ内に言語別 `Analysis` を複数保持できる（`ja` と `en` 共存）
- [ ] `IsFavorite` をトグルできる
- [ ] Entry を削除できる
- [ ] 一覧が `ViewedAt` 降順で返る（SPEC 2.5）
- [ ] キャッシュ判定: 指定 `full_name` × 言語の Analysis 有無を返せる（SPEC 2.6）
- [ ] 書き込みは一時ファイル + rename でアトミック（途中失敗で `data.json` が壊れない）

> 並行書き込みは想定外（単一プロセス TUI）。まずは直列前提で実装し、必要になれば mutex を足す。

---

## フェーズ 3: clients/github（GitHub REST）

**ゴール:** 入力パースと REST 取得・エラー分岐を、httptest モックで検証する。

### 3.1 owner/repo パース — テストリスト
- [ ] `"owner/repo"` を `(owner, repo)` に分解する
- [ ] `https://github.com/owner/repo` を分解する（末尾スラッシュ / `.git` も許容）
- [ ] 空文字・スラッシュ無し・3階層以上は明確なエラーにする

### 3.2 REST 取得 — テストリスト（httptest でスタブ）
- [ ] `GET /repos/{o}/{r}` 200 を RepoMeta にマッピングする
- [ ] `GET /repos/{o}/{r}/languages` を言語構成にマッピングする
- [ ] `GET /repos/{o}/{r}/readme` を `Accept: application/vnd.github.raw` で取得し生 Markdown を得る
- [ ] トークンありのとき `Authorization: Bearer <token>` を付与する
- [ ] 404 を「リポジトリが見つからない」と区別してメッセージ化する
- [ ] レート制限（403 + `X-RateLimit-Remaining: 0`）を専用メッセージにする
- [ ] その他 5xx を汎用エラーにする

> `http.Client` の baseURL を差し替え可能にし、テストでは `httptest.NewServer` を指す。

---

## フェーズ 4: clients/ai（AI 抽象・プロンプト・JSON 抽出）

**ゴール:** プロバイダに依存しない「プロンプト構築」「JSON 抽出」「Analysis 変換」を固める。
ここは純粋ロジックが多く TDD が最も効く。

### ai.go — テストリスト
- [ ] system プロンプトに出力言語（ja/en）と JSON 形式指定が含まれる
- [ ] user プロンプトに RepoMeta（名前・説明・スター・言語・トピック）が含まれる
- [ ] README が上限文字数で切り詰められる（SPEC 3章 / 6.2）
- [ ] 切り詰め上限未満の README はそのまま渡る
- [ ] JSON 抽出: 前後の説明文を除去し `{ ... }` だけを取り出す
- [ ] JSON 抽出: ` ```json ... ``` ` コードフェンスを剥がす
- [ ] JSON 抽出: 最初の `{` から最後の `}` までを対象にする
- [ ] 抽出した JSON を `Analysis`（要約・技術解説・技術的背景・キーワード配列）へパースする
- [ ] JSON として不正なら明確なエラーを返す
- [ ] プロバイダ生成: `"claude"` で Claude 実装、`"openai"` で OpenAI 実装を返す
- [ ] 未知プロバイダ名はエラーにする

> `Provider` インターフェース（例: `Generate(ctx, meta, readme, lang) (*Analysis, error)`）を定義し、
> claude/openai を差し替え可能にする。

---

## フェーズ 5: clients/claude・openai（各 API 実装）

**ゴール:** リクエスト形状とレスポンス解析を httptest で検証する。実キーは使わない。

### claude.go — テストリスト
- [ ] Messages API 形状（model / system / messages / max_tokens）で POST する
- [ ] `x-api-key` と `anthropic-version` ヘッダを付与する
- [ ] 正常レスポンスから本文テキストを取り出し Analysis まで通す
- [ ] API エラー（4xx/5xx）をメッセージ化する

### openai.go — テストリスト
- [ ] Chat Completions 形状（model / messages）で POST する
- [ ] `response_format: {type: json_object}` を付与する（SPEC 6.2）
- [ ] `Authorization: Bearer` を付与する
- [ ] 正常レスポンスから本文を取り出し Analysis まで通す
- [ ] API エラーをメッセージ化する

> モデル名・エンドポイント・料金は投入前に各社公式ドキュメントで再確認（SPEC 9.2）。

---

## フェーズ 6: tui（Bubble Tea）

**ゴール:** 状態遷移とキー処理の**ロジック**をテストし、既知の最優先バグを潰す。
描画（view.go）は golden/目視に寄せ、テストは update に集中する。

### 6.1 【最優先バグ】入力欄のショートカット横取り — テストリスト
SPEC 9.2 記載の既知バグ。`l p f d q` を含む repo 名（`prettier/prettier`, `denoland/deno`）が打てない。
- [ ] 入力欄が**空**のとき `l` は言語切替として扱われる
- [ ] 入力欄が**空**のとき `p` はプロバイダ切替として扱われる
- [ ] 入力欄が**空**のとき `f`/`d`/`q` がショートカットとして扱われる
- [ ] 入力欄に**文字がある**とき `p` は入力へ流れる（`prettier` が打てる）
- [ ] 入力欄に**文字がある**とき `d`/`l` などが入力へ流れる（`denoland/deno` が打てる）
- [ ] 修正対象は `internal/tui/update.go` の `updateInput`（空判定で分岐し、非空は `m.input.Update(msg)` へ）

### 6.2 状態遷移 — テストリスト
- [ ] 初期状態は `stateInput`
- [ ] Enter（有効な入力）で `stateLoading` へ遷移する
- [ ] 取得・生成完了メッセージで `stateDetail` へ遷移する
- [ ] `stateLoading` 中の esc でキャンセルして `stateInput` へ戻る
- [ ] `stateDetail` の esc で `stateInput` へ戻る
- [ ] `tab` で履歴 ⇄ お気に入りの表示リストが切り替わる
- [ ] `f` で選択エントリのお気に入りが反転する
- [ ] 詳細画面の `l` で言語切替、未生成言語なら再生成フローに入る
- [ ] `r` で強制再生成フラグ付きの取得コマンドを発行する

### 6.3 描画（テスト薄め）
- [ ] Glamour で Markdown をレンダリングして表示（目視確認）
- [ ] スクロール（viewport）が上下 / PgUp / PgDn で動く（目視確認）

---

## フェーズ 7: commands（非同期処理フロー）

**ゴール:** 「キャッシュ優先 → 取得 → 生成 → 保存」フロー（SPEC 4.3）を検証する。
tui の Cmd を、依存（store / github / ai）をインターフェース注入してテスト可能にする。

### commands.go — テストリスト
- [ ] キャッシュあり（同一 repo × 同一言語）は AI を呼ばず即返し、`ViewedAt` のみ更新する
- [ ] キャッシュなしは GitHub 取得 → store upsert → AI 生成 → 保存の順で進む
- [ ] 強制再生成（`r`）はキャッシュがあっても AI を呼ぶ
- [ ] 取得失敗時はエラーメッセージ Msg を返し、`stateInput` に戻せる

---

## フェーズ 8: cmd（cobra CLI / ウィザード）

**ゴール:** サブコマンドの分岐と設定ウィザードの保存を確認する。

- [ ] `reporepo`（引数なし）で TUI を起動する
- [ ] `reporepo config` で対話ウィザードを起動し、入力値を 0600 で保存する（SPEC 2.7）
- [ ] `reporepo version` でバージョンを表示する
- [ ] `reporepo where` で `data.json` / 設定ファイルのパスを表示する
- [ ] 環境変数がある場合は実行時にファイル値より優先される（config 側テストと整合）

> ウィザードの対話部分は入出力を差し替え可能にして最小限テストする。分岐ロジックが主対象。

---

## フェーズ 9: 結合・ビルド検証・配布

**ゴール:** SPEC 9.2 の「ビルド未検証」を解消し、実際に動く単一バイナリにする。

- [ ] `go build ./...` / `go vet ./...` / `go test ./...` が全て緑
- [ ] ライブラリ現行版との API 差異を吸収（Bubble Tea/Bubbles/Glamour/Lipgloss/Cobra）
- [ ] 実キーでのスモークテスト（実在 repo を1件解析 → 保存 → 再閲覧がキャッシュヒット）
- [ ] `l p f d q` を含む repo 名の入力を手動確認（バグ修正の最終確認）
- [ ] module path `github.com/yourname/reporepo` を実際の公開先へ置換（SPEC 9.2）
- [ ] LICENSE 著作者名を実名へ置換
- [ ] `Makefile cross` で darwin/linux/windows × amd64/arm64 をビルド（CGO 不要を確認）
- [ ] GoReleaser 設定をタグ push で検証（アーカイブ + チェックサム生成）

---

## 実装順まとめ（クリティカルパス）

| 順 | フェーズ | 主眼 | TDD 密度 |
|----|----------|------|----------|
| 0 | 初期化 | ビルド土台 | なし |
| 1 | core/config | パス解決・env 優先・0600 | 高 |
| 2 | store | 同一 repo 集約・アトミック書込 | 高 |
| 3 | github | パース・REST・エラー分岐 | 高（httptest）|
| 4 | ai 抽象 | プロンプト・JSON 抽出 | 最高 |
| 5 | claude/openai | リクエスト形状・解析 | 中（httptest）|
| 6 | tui | **最優先バグ修正**・状態遷移 | 中 |
| 7 | commands | キャッシュ優先フロー | 中 |
| 8 | cmd | CLI 分岐・ウィザード | 中 |
| 9 | 結合/配布 | ビルド検証・クロスコンパイル | 手動 |

## 最優先で着手する3点（SPEC 9.2 由来のリスク）

1. **ビルド未検証** → フェーズ0で最小構成をビルドし、以降 常時緑を維持。
2. **入力欄のショートカット横取りバグ** → フェーズ6.1 でテストから修正。
3. **モデル名 / API 仕様の現行確認** → フェーズ4〜5の実キー投入前に公式ドキュメントで検証。
