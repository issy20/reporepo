# Plan: AI clients（Path A: 実行可能な契約）

Status: done

## ゴール

Claude Messages API と OpenAI Chat Completions API を共通の `AIClient` 境界で利用し、構造化された `core.Analysis` を返せるようにする。

## 設計境界

- `AIClient.Generate` が TUI と外部 API の境界。
- コンストラクタで API key、model、`*http.Client` を注入する。
- prompt 生成とモデル出力からの JSON 抽出は `ai.go` に集約する。
- README は Unicode の文字単位で 12,000 文字までに制限する。
- API key はエラーへ含めない。

## テストリスト

- [x] system prompt が出力言語と必須 JSON フィールドを指定する
- [x] user prompt が RepoMeta を含み、README を rune 単位で上限まで切り詰める
- [x] nil metadata、空の full_name、未対応言語を拒否する
- [x] 前後文・コードフェンス付きの JSON object を抽出する
- [x] JSON 不在、壊れた JSON、必須フィールド不足を拒否する
- [x] Claude が Messages API の認証ヘッダと request body を送る
- [x] Claude の text content を `Analysis` に変換する
- [x] Claude の非 2xx を秘密情報なしのエラーにする
- [x] OpenAI が Bearer 認証と `response_format: json_object` を送る
- [x] OpenAI の最初の choice を `Analysis` に変換する
- [x] OpenAI の空 choices と非 2xx をエラーにする

## TDD結果

`ErrNotImplemented` を返すスタブで正常系4テストの red を確認後、公開シグネチャを変更せず全テストを green にした。レビューで追加した API key 漏洩テストも green。
