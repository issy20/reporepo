# Reporepo 仕様書 兼 実装計画

最終更新: 2026-06-25
バージョン: 0.1.0 (開発中)
ステータス: コア実装完了 / ビルド未検証 / 既知バグ未修正

---

## 1. 概要

Reporepo は、GitHub リポジトリ名（`owner/repo`）を入力すると、GitHub から情報を取得し、AI（Claude / OpenAI）が「要約・技術解説・技術的背景」を生成して表示する **TUI（ターミナルUI）アプリ**である。生成結果はローカルに保存され、履歴・お気に入りから再閲覧できる。最先端の技術やトレンドを、リポジトリ単位で学習することを目的とする。

### 設計の背景にある意思決定

このプロジェクトは当初「GitHub Trending を取得して AI でまとめる Web アプリ」として構想されたが、検討の結果、次の理由で形を変えている。

GitHub には公式の Trending API が存在しない。スクレイピングや Search API による擬似 Trending は可能だが不安定・近似的である。そこで入力を `owner/repo` 文字列に限定し、確実に動く公式 REST API のみを使う方針とした。Trending は将来「入力のネタ探し」として後付けできる位置づけとした。

配布形態は Web アプリではなく TUI を選んだ。理由は「認証なし・自前 API キー・生成結果の保存」という要件にある。Web アプリでサーバーを立てると、認証なしのエンドポイントは第三者に叩かれ、運営者の API キーが濫用される。TUI なら各ユーザーが自分の端末で自分のキーを使うため、この問題が構造的に消滅する。認証もサーバーも不要になる。

技術スタックは Go + Bubble Tea を採用した。単一バイナリにコンパイルして配布でき、ユーザー側の環境構築が不要なため。

データ保存は SQLite ではなく JSON ファイルを採用した。`mattn/go-sqlite3` は CGO 依存でクロスコンパイルを阻害し、Go 採用の最大の利点（単一バイナリ配布）を削ぐため。個人の学習ノート規模では JSON 全読み込みで十分実用的であり、依存ゼロでクロスコンパイルが自由になる。

---

## 2. 確定した機能要件

### 2.1 コア機能

`owner/repo`（または GitHub URL）を入力して解析を実行する。GitHub からリポジトリのメタ情報、README、言語構成を取得し、AI に渡して構造化された解説（要約・技術解説・技術的背景・キーワード）を生成し、整形表示する。

### 2.2 AI プロバイダ

Claude（Anthropic Messages API）と OpenAI（Chat Completions API）の両方に対応し、実行時に `p` キーでトグルできる。出力は JSON 構造化出力を強制し、UI のセクションへマッピングする。

### 2.3 多言語

日本語と英語を `l` キーで切り替える。生成結果は言語ごとに別々にキャッシュする（同じ repo でも日英は別エントリ）。

### 2.4 保存と履歴

生成結果は JSON ファイルに保存する。**同じ repo は1エントリにまとめ、再閲覧時は最新閲覧日時を更新する**（履歴が重複行で膨らまない）。1エントリ内に言語別の解析結果を複数保持する。

### 2.5 履歴 / お気に入り

`tab` キーで「履歴」「お気に入り」を切り替える。`f` キーでお気に入りをトグルする。リストは最新閲覧日時の降順で表示する。

### 2.6 キャッシュ優先

一度生成した repo（同一言語）は、再取得・再生成せず保存済みを即時表示する。これによりコストとレイテンシを抑える。`r` キーで強制再生成できる。

### 2.7 設定

`reporepo config` で対話的に API キー等を設定する。環境変数（`GITHUB_TOKEN` / `ANTHROPIC_API_KEY` / `OPENAI_API_KEY`）があれば実行時に優先する。

---

## 3. 非機能要件

API キーはローカル設定ファイルにパーミッション 0600 で保存する。サーバー・認証は持たない（各ユーザーが自分のキーを使うため不要）。単一バイナリで配布でき、CGO 不要でクロスコンパイル可能とする。GitHub トークン未設定でも動作するが、その場合は 60 リクエスト/時の制限がかかる。README は AI に渡す前に文字数で切り詰め、コストとレイテンシを抑制する。

---

## 4. アーキテクチャ

### 4.1 レイヤ構成

最下層に設定管理（`internal/core/config.go`）と永続化（`internal/store`）を置く。その上に外部クライアント（`internal/clients`: GitHub、Claude、OpenAI）を置く。最上層に TUI（`internal/tui`）を置き、CLI エントリ（`cmd`）がそれらを束ねる。

### 4.2 ディレクトリ構成

```
reporepo/
  main.go                      エントリポイント
  go.mod                       依存定義（JSONのみ、CGO不要）
  Makefile                     ビルド・クロスコンパイル
  .goreleaser.yaml             リリース自動化設定
  README.md / LICENSE / .gitignore
  cmd/
    root.go                    cobra ルートコマンド（run/config/version/where）
    wizard.go                  設定ウィザード
  internal/
    core/
      types.go                 データ型（Entry / RepoMeta / Analysis）
      config.go                設定の読み書き、パス解決
    store/
      store.go                 JSON 永続化ストア（同一 repo を1行にまとめる）
    clients/
      github.go                GitHub REST クライアント、owner/repo パース
      ai.go                    AI 抽象、プロンプト構築、JSON 抽出、プロバイダ生成
      claude.go                Claude Messages API 実装
      openai.go                OpenAI Chat Completions 実装
    tui/
      model.go                 Bubble Tea モデル定義と初期化
      update.go                キー処理・状態遷移
      view.go                  描画・Markdown レンダリング
      commands.go              非同期コマンド（取得→生成→保存）
      styles.go                lipgloss スタイル
      run.go                   TUI 起動関数
```

### 4.3 処理フロー（解析実行時）

入力をパースして `owner/repo` を得る。ストアにキャッシュがあるか確認し、あれば AI を呼ばず即返す（閲覧日時のみ更新）。なければ GitHub からメタ・README・言語構成を取得し、ストアに upsert する。選択中のプロバイダで AI 生成を行い、結果をストアに保存して表示する。

### 4.4 状態遷移（TUI）

3つの状態を持つ。`stateInput`（owner/repo 入力とリスト表示）、`stateLoading`（取得・生成中、スピナー表示、esc でキャンセル）、`stateDetail`（解説のスクロール表示）。

---

## 5. データモデル

`Entry` がストアの主レコードで、主キーは `full_name`（owner/repo）。1つの `RepoMeta`、言語をキーとする `Analyses`（`map[string]*Analysis`）、`IsFavorite`、`ViewedAt`（最新閲覧日時）、`CreatedAt`（初回登録）を持つ。

`Analysis` は言語別の生成結果で、要約・技術解説・技術的背景・キーワード配列・言語・プロバイダ・モデル・生成日時を持つ。

`RepoMeta` は GitHub 由来のメタ情報で、フルネーム・説明・スター数・フォーク数・主要言語・トピック・言語構成・URL・ライセンス・更新日時を持つ。

保存先は `data.json`（`$XDG_DATA_HOME/reporepo/` または `~/.local/share/reporepo/`）に `Entry` の配列として書き出す。書き込みは一時ファイル経由の rename でアトミックに行う。

---

## 6. 外部 API 仕様

### 6.1 GitHub REST

`GET /repos/{owner}/{repo}` で基本情報、`GET /repos/{owner}/{repo}/languages` で言語構成、`GET /repos/{owner}/{repo}/readme`（Accept: `application/vnd.github.raw`）で生 Markdown を取得する。トークンがあれば `Authorization: Bearer` を付与し、レート制限を 5000 req/h に引き上げる。404・レート制限・その他エラーを区別してメッセージ化する。

### 6.2 AI

両プロバイダとも system プロンプトで出力言語と JSON 形式を指定し、user プロンプトにリポジトリのメタ情報と切り詰めた README を渡す。出力はコードフェンスや前後テキストを除去してから最初の `{` から最後の `}` を抽出してパースする（モデルが余計な文字を付けても吸収するため）。OpenAI は `response_format: json_object` も併用する。

---

## 7. キー操作仕様

入力画面では、Enter で解析またはリスト選択を開く、上下キーでリスト選択、tab で履歴⇄お気に入り、f でお気に入りトグル、d で削除、l で言語切替、p で AI 切替、q/esc で終了。詳細画面では、上下/PgUp/PgDn でスクロール、l で言語切替（必要なら再生成）、f でお気に入り、r で強制再生成、esc で戻る。

---

## 8. 配布計画

Makefile の `cross` ターゲットで darwin/linux/windows × amd64/arm64 の単一バイナリを一括ビルドする。GoReleaser でタグ push をトリガにリリースを自動化し、各 OS 向けアーカイブとチェックサムを生成する。配布手段は GitHub Releases のビルド済みバイナリ、`go install`、（任意で）Homebrew tap を想定する。

---

## 9. 実装状況

### 9.1 完了

データ型、設定の読み書き、JSON ストア（同一 repo を1行にまとめるロジック含む）、GitHub クライアント、Claude / OpenAI クライアント、AI 抽象とプロンプト構築、TUI のモデル・更新・描画・非同期コマンド・スタイル、cobra CLI、設定ウィザード、README・Makefile・GoReleaser 設定・LICENSE。総行数は約 1,750 行。

### 9.2 未完了 / 要対応

以下は引き継ぎ時に必ず対応すること。

**ビルド未検証。** 開発環境に Go が無くネットワークも無効だったため、`go build` と `go mod tidy` を一度も実行できていない。最初のビルドはデバッグ前提で臨むこと。ライブラリ（Bubble Tea, Bubbles, Glamour, Lipgloss, Cobra）のバージョンは go.mod に固定値を書いたが、現行版との差異で API が変わっている可能性がある。

**既知のバグ（最優先）。** 入力画面で `l` `p` `f` `d` `q` を常にショートカットとして横取りしているため、これらの文字を含む repo 名（例: `prettier/prettier`, `denoland/deno`）を入力欄にタイプできない。修正方針は、入力欄が空のときだけこれらをショートカットとして扱い、文字入力中は `m.input.Update(msg)` へ流すこと。対象は `internal/tui/update.go` の `updateInput`。

**モデル名と API 仕様の検証。** 設定の既定モデル名（`claude-sonnet-4-6`, `gpt-4o-mini`）と各 API のリクエスト形状は、実キーを入れる前に各社の公式ドキュメントで現行仕様を確認すること。料金体系も変動する。

**プレースホルダの module path。** `go.mod` と全 import の `github.com/yourname/reporepo` は仮の名前。実際の公開先に置換すること。LICENSE の著作者名も同様。

---

## 10. 今後の拡張余地

擬似 Trending 一覧（GitHub Search API で「直近作成 × 高スター」を取得し、選択して詳細解説へ遷移）を追加できる。入力を `owner/repo` 文字列に抽象化済みのため、入力元がフォームでも一覧でも同じ処理に流せる。その他、全文検索、Markdown ノートのエクスポート、ローカル LLM 対応、キーワードからの関連リポジトリ推薦などが考えられる。

---

## 11. オープンな論点

将来 Web 公開する場合は、認証なしが成り立つのは自分専用のときだけである点に注意する。公開するなら合言葉1個（共有シークレットのヘッダ照合）から始め、本格公開時に初めて本物の認証・レート制限・課金分離を設計する。この段階的方針はドキュメントとして合意済み。
