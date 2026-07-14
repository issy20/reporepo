# Plan: GitHub REST クライアント

Status: done   <!-- draft -> ready(Codex承認:設計&テスト) -> doing -> done(Codex承認:実装) -->

## ゴール
`owner/repo` または GitHub URL を正規化し、GitHub REST API からメタ情報・言語構成・READMEを取得する境界をTDDで構築する。

## 設計
- `ParseRepositoryInput(input string) (owner, repo string, err error)` が入力形式の検証と正規化を担当する。
- `GitHubClient` は `FetchRepository(ctx, owner, repo) (*RepositoryData, error)` のみを公開する。
- `RepositoryData` は `Meta *core.RepoMeta` と生Markdownの `README string` を保持する。
- `Client` は `http.Client`、base URL、任意のtokenを注入可能とし、テストで実ネットワークを使わない。
- 404とレート制限は `errors.Is` で判別可能にし、その他の非2xxはstatus codeを保持する `HTTPError` とする。

## テストリスト
- [x] `owner/repo` をownerとrepoへ分解できる
- [x] GitHub URLを分解でき、末尾スラッシュと `.git` を正規化できる
- [x] 空文字、階層不足・過多、GitHub以外のURLを拒否する
- [x] repository、languages、readmeの3エンドポイントを取得して `RepositoryData` に統合する
- [x] token指定時に全リクエストへ `Authorization: Bearer` を付ける
- [x] README取得時にraw media typeの `Accept` を付ける
- [x] 404を `ErrNotFound` に分類する
- [x] rate limit応答を `ErrRateLimited` に分類する
- [x] その他の非2xxをstatus code付き `HTTPError` にする
- [x] context cancellationを呼び出し元へ返す
- [x] owner/repo の前後空白を正規化し、空白・`?`・`#`・エスケープ済みslashなどURLパスを変形する文字を拒否する
- [x] `FetchRepository` に直接不正なowner/repoが渡されても、別パスやqueryへ変形されない
- [x] nilの `http.Client` を渡した場合にpanicしない契約を定める（デフォルト採用または明示的エラー）
- [x] `ParseRepositoryInput` と `FetchRepository` が同じsegment検証を共有し、`.`・`..`・percent encoding・制御文字を一貫して拒否する

## Path A handoff
- インターフェースとスタブのみ作成する。具体的なHTTP処理・パース処理は実装しない。
- 全テストがコンパイルでき、スタブの `ErrNotImplemented` により失敗するredを確定する。

## Codex 実装レビュー (2026-07-14)
- 判定: 差し戻し (`doing`)。通常・race・vetは成功したが、URLパス境界の入力検証とnil依存の契約が未確定。
- 再レビュー: 差し戻し (`doing`)。前回指摘は修正済みだが、parseとfetchの検証が不一致で、parseがpath traversal表現を受理する。
- 最終受け入れ: 承認 (`done`)。共通segment検証へ統合し、追加した境界テスト、通常・race・vetがすべて成功。
