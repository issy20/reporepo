# Plan: 配布可能にする（GoReleaser・LICENSE・CI）

Status: draft

## 目的

SPEC 8章 配布計画の実現に向けた土台を整える。リポジトリには `.goreleaser.yaml` と `LICENSE` が存在せず、CI も無いため、GitHub Releases への自動リリースを可能にする設定、配布に必要なライセンス、回帰テストとリリースを自動化する CI ワークフローを追加する。

> 注: リポジトリは現在 **private** のため、GitHub Actions は無料枠（月約2,000分）を超えると従量課金になる。テスト用・リリース用のワークフローは軽量・低頻度のため、実用上無料枠に収まる見込み。リポジトリを public にした場合はさらに安心。

## 前提

- LICENSE は **MIT** を適用する。
- バージョンは現状 `cmd/root.go` の `const version = "0.1.0"` にハードコードされている。**GoReleaser は `-ldflags` でこの定数が参照する `version` の値をビルド時に上書き**する方針にする（`const` は `-ldflags` で上書きできないため、`var` へ変更が必要）。
- 配布対象は SPEC 2章・Makefile の `PLATFORMS` と同じ darwin / linux / windows × amd64 / arm64。
- 単一バイナリ配布（CGO 不要）を維持する。`CGO_ENABLED=0`。
- リリースのトリガは GoReleaser 標準どおり「タグ push」とする。タグ運用は v0.1.0 から始める（既存タグなし）。

## スコープ

### 対象

- `.goreleaser.yaml`: 新規追加（ビルド・アーカイブ・チェックサム設定）
- `LICENSE`: 新規追加（MIT、著作権者 `issy20`、年 2026）
- `cmd/root.go`: `const version` → `var version` へ変更（`-ldflags` 注入のため）
- `.github/workflows/ci.yml`: 新規追加（PR / push でテスト・vet を実行）
- `.github/workflows/release.yml`: 新規追加（`v*` タグ push で GoReleaser を実行し GitHub Releases へ公開）
- `Makefile`: 必要なら `release` ターゲットを追加（任意）
- ドキュメント: SPEC 8章・README へリリース手順を追記

### 対象外

- Homebrew tap の設定（任意・将来）
- GitHub Actions のワークフロー追加（タグ push の自動ビルドは GoReleaser の CLI 自体で対応。CI への組み込みは別実施）
- 署名（sigstore / cosign）の精緻化（`v0.1.0` では省略可、将来対応）
- version の `const` → `var` に伴う表示テストの変更（`version` コマンドの出力は変わらないことを確認）

## 設計

### 変更1: LICENSE（MIT）

`LICENSE` ファイルを追加する。MIT ライセンス全文を、著作権者 `Copyright (c) 2026 issy20` で記載する。

### 変更2: バージョン注入のための `const` → `var`

`cmd/root.go:14` の `const version = "0.1.0"` を `var version = "0.1.0"` へ変更する。`-ldflags "-X main.version=..."` 相当のGoReleaser の `ldflags` 設定で、ビルド時にタグのバージョンへ上書きできるようにする。

> 注: `version` は `cmd` パッケージ内の変数であり、`main` パッケージではない。GoReleaser の `ldflags` は `main.version` をデフォルト対象とする。cmd パッケージ配下の変数へ注入する場合は、GoReleaser の `ldflags` テンプレートで `github.com/issy20/reporepo/cmd.version` を指定する。

### 変更3: .goreleaser.yaml

以下の内容で新規作成する。

```yaml
version: 2

project_name: reporepo

before:
  hooks:
    - go mod tidy

builds:
  - env:
      - CGO_ENABLED=0
    main: .
    ldflags:
      - -s -w -X github.com/issy20/reporepo/cmd.version={{ .Version }}
    goos:
      - darwin
      - linux
      - windows
    goarch:
      - amd64
      - arm64

archives:
  - format: tar.gz
    format_overrides:
      - goos: windows
        format: zip
    files:
      - LICENSE
      - README.md

checksum:
  name_template: 'checksums.txt'

snapshot:
  name_template: '{{ incpatch .Version }}-next'

changelog:
  sort: asc
  filters:
    exclude:
      - '^docs:'
      - '^test:'
```

- アーカイブには LICENSE / README.md を含める
- `checksums.txt` でチェックサムを生成
- changelog から docs / test を除外

### 変更4: ドキュメント・Makefile（任意）

- `Makefile` へ `release` ターゲットを追加し、`goreleaser release --clean`（または `--snapshot` でのテスト）を派生的に実行できるようにする（GoReleaser が未導入の場合はコメントで手順を記載）。
- README の「リリース」節と SPEC 8章に、タグ push 後の GoReleaser 実行手順を追記する。

### 変更5: CI ワークフロー（.github/workflows/）

**ci.yml（回帰テスト）。** PR と main への push で実行する。

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
      - run: go vet ./...
      - run: go test ./...
      - run: go test -race ./...
```

**release.yml（自動リリース）。** `v*` タグ push で GoReleaser を実行し、GitHub Releases へビルド済みバイナリを公開する。

```yaml
name: Release

on:
  push:
    tags:
      - 'v*'

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
      - uses: goreleaser/goreleaser-action@v6
        with:
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

- `GITHUB_TOKEN` はリポジトリ内蔵のため追加の secret は不要
- release はタグ push のみで発火し、通常はテスト済みの内容をリリースする（必要なら `ci.yml` を `required status check` にするのは将来の強化）

## テストリスト

### A. バージョン注入（cmd）

- [ ] `version` コマンドの既定出力が `reporepo 0.1.0` のままである（回帰）
- [ ] `-ldflags "-X github.com/issy20/reporepo/cmd.version=v1.2.3-test"` でビルドしたバイナリの `version` 出力が `reporepo v1.2.3-test` になる
- [ ] 既存の main integration test（`version` 期待 `reporepo 0.1.0`）が通る（回帰）

### B. 設定の妥当性

- [ ] `.goreleaser.yaml` が GoReleaser のスキーマ検証を通る（`goreleaser check`）
- [ ] `goreleaser build --snapshot` が成功し、`dist/` に各プラットフォームのバイナリが生成される（可能なら）
- [ ] `LICENSE` が存在し、`cargo` 等の検証が通る（存在確認で十分）

### C. ドキュメント

- [ ] SPEC 8章・README に GoReleaser リリース手順が追記されている

### D. CI

- [ ] `.github/workflows/ci.yml` が `go test ./...` / `-race` / `go vet` を実行する
- [ ] `.github/workflows/release.yml` が `v*` タグ push で GoReleaser を実行する
- [ ] ワークフローのYAMLが妥当である（`actionlint` 等で確認可能）

## 実装順序

### Step 1: LICENSE 追加

MIT ライセンス全文を `LICENSE` に追加。

### Step 2: バージョン注入（const → var）

`cmd/root.go` の `const version` を `var version` へ変更し、既存テストが通ることを確認。`-ldflags` での上書きをローカルで検証。

### Step 3: .goreleaser.yaml 追加

`goreleaser check` でスキーマを検証。未導入なら `go install github.com/goreleaser/goreleaser/v2@latest` を案内。

### Step 4: CI ワークフロー追加

`.github/workflows/ci.yml` / `release.yml` を追加。

### Step 5: ドキュメント更新

SPEC 8章・README へ手順を追記。

### Step 6: 検証

```bash
gofmt -l .
go test ./...
go test -race ./...
go vet ./...
# 可能なら
goreleaser check
```

## 完了条件

- `LICENSE`（MIT）が存在する
- `version` がビルド時に `-ldflags` で注入でき、既定値 `0.1.0` のままである
- `.goreleaser.yaml` が妥当で、タグ push からのリリースが可能
- 既存テストが全て通る

## 想定される変更

- `LICENSE`: 新規追加
- `.goreleaser.yaml`: 新規追加
- `.github/workflows/ci.yml`: 新規追加
- `.github/workflows/release.yml`: 新規追加
- `cmd/root.go`: `const version` → `var version`
- `Makefile`: `release` ターゲット（任意）
- `SPEC.md`（8章） / `README.md`: リリース手順追記

## worktree

- branch: `dist-release-setup`
- worktree path: `/Users/issy20/ccplayground/reporepo/dist-release-setup`

理由: SPEC 8章 配布計画の実現。LICENSE・GoReleaser設定・バージョン注入・CIワークフローという独立したテーマで、`cmd` の1行変更と新規ファイル追加に収まるため、専用 worktree で進める。
