# Plan: フェーズ1 & フェーズ2 (core/config & store の実装)

Status: done   <!-- draft -> ready(Codex承認:設計&テスト) -> doing -> done(Codex承認:実装) -->

## ゴール (What / Why) ― 3行以内
Reporepoのコアデータ構造（types）、設定管理（config）、およびJSON永続化ストア（store）をTDDで実装し、以降のTUI/API実装の強固な土台を構築する。

## 対象ファイル
- `internal/core/types.go` ― 新規作成 (データモデル定義)
- `internal/core/config.go` ― 新規作成 (設定の読み書き、パス解決、環境変数優先)
- `internal/core/config_test.go` ― 新規作成 (config のユニットテスト)
- `internal/store/store.go` ― 新規作成 (JSON永続化、同一repoの集約、アトミック書き込み)
- `internal/store/store_test.go` ― 新規作成 (store のユニットテスト)

## 変更方針 (How) ― ファイル単位、Codex が再探索せず読める粒度で
### `internal/core/types.go`
- `Entry` (`full_name`, `RepoMeta`, `Analyses map[string]*Analysis`, `IsFavorite`, `ViewedAt`, `CreatedAt`)
- `Analysis` (`Summary`, `TechStack`, `Background`, `Keywords []string`, `Language`, `Provider`, `Model`, `CreatedAt`)
- `RepoMeta` (`FullName`, `Description`, `Stars`, `Forks`, `Language`, `Topics []string`, `Languages map[string]int`, `URL`, `License`, `UpdatedAt`)

### `internal/core/config.go`
- `Config` 構造体 (`GithubToken`, `AnthropicAPIKey`, `OpenAIAPIKey`, `DefaultProvider`, `DefaultLanguage`)
- `LoadConfig()`: `$XDG_CONFIG_HOME/reporepo/config.json` から読み込み。存在しない場合はデフォルト値を返す。環境変数優先。
- `SaveConfig(cfg *Config)`: 設定を 0600 でアトミック保存。

### `internal/store/store.go`
- `Store` 構造体 (`filepath` を保持)
- `Load()`: `data.json` から全 `Entry` を読み込む。
- `Save(entries []*Entry)`: 一時ファイルに書き込んでから `os.Rename` でアトミックに保存。
- `Upsert(entry *Entry)`: 同一 `full_name` があればマージ（`ViewedAt` 更新、`Analyses` マージ）、なければ新規追加。

## テスト (TDD)
- [x] config: 存在しない設定ファイル読み込みでデフォルト値が返ること
- [x] config: 保存時にファイルパーミッションが 0600 であること
- [x] config: 環境変数が設定ファイルの値より優先されること
- テスト先: `internal/core/config_test.go` ／ 実行: `go test ./internal/core/...`

- [x] store: 新規エントリの保存とロードができること
- [x] store: 同一リポジトリ（full_name）のUpsertで, 重複せずマージされ `ViewedAt` が更新されること
- [x] store: 保存がアトミックに行われること（一時ファイル経由。失敗時に既存ファイルを保持し、一時ファイルを残さないこと）
- [x] store: `Upsert(nil)` および保存済みデータ内の `null` を panic せずエラーとして扱うこと
- [x] config: 実行中に `XDG_CONFIG_HOME` が変わっても、その時点の標準パスを解決すること（プロセス全体の可変キャッシュを持たないこと）
- [x] config/store: 既存ファイルへの再保存が対応対象 OS（特に Windows）でも成功すること
- テスト先: `internal/store/store_test.go` ／ 実行: `go test ./internal/store/...`

## タスク ― 実装しながらチェック
- [x] `internal/core/types.go` の定義作成
- [x] `internal/core/config_test.go` で失敗するテスト (Red) を記述
- [x] `internal/core/config.go` を実装してテストを成功させる (Green)
- [x] `internal/store/store_test.go` で失敗するテスト (Red) を記述
- [x] `internal/store/store.go` を実装してテストを成功させる (Green)
- [x] 全テストが成功することを確認し、リファクタリングを実施

## Codex 事後レビュー (2026-07-11)
- 判定: 設計・テストリスト承認 (`ready`)。通常テスト・race・vet は成功したが、上記の未実装テストと異常系を解消後に実装レビューする。
- 最終レビュー: 差し戻し (`doing`)。アトミック保存の失敗テストが置換処理まで到達せず、Windows 上の再保存も検証されていない。
- 再レビュー: 差し戻し (`doing`)。rename 失敗時の一時ファイル削除は確認できるが、既存ファイル保持と Windows 実行時の再保存成功は未検証。
- 最終受け入れ: 承認 (`done`)。置換失敗を注入するテストで既存ファイル保持と一時ファイル削除を確認し、通常・race・vet・Windows 向けコンパイルが成功。

## 注意 ― 調査で気づいた制約・落穴
- 設定ファイルおよびデータファイルの保存先は、OS（Mac/Linux/Windows）ごとに標準的なパス（`os.UserConfigDir()`, `os.UserCacheDir()` 等）を利用するように `internal/core/config.go` で解決させる。
- 本実装は、ユーザーが手動で作成した `feature-config-store` ワークツリー上で行います（Gitの作成・コミット・削除等の操作はすべてユーザーが手動で行うため、手順からは除外しています）。
