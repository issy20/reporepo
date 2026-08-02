# Plan: SecretStoreのin-memoryテスト基盤

Status: implemented

## 目的

OS Keychain、Windows Credential Manager、Secret Serviceへ依存するsecret管理のテストを、プロセス内で完結するin-memory実装へ統一する。

本番では引き続き `secretstore.NewKeyringStore()` を利用する。一方、ユースケース・CLI・設定ウィザード・旧設定移行の自動テストでは実OS資格情報ストアを一切読み書きしない。

以下を保証する。

- 開発OSやCI環境に関係なく同じテスト結果になる。
- OSのpermission dialog、D-Bus session、desktop sessionを必要としない。
- テスト実行時に利用者の実credentialを参照・変更しない。
- Get / Set / Deleteの成功、未登録、backend障害を決定的に再現できる。
- rollbackと操作順序を状態および呼び出し履歴から検証できる。
- Keyring adapterの薄い変換ロジックだけはbackend stubで検証する。

## 現状

- 本番composition rootは `cmd/root.go` で `secretstore.NewKeyringStore()` を生成している。
- `internal/secretstore/keyring_test.go` は関数stubを注入し、OS backendを呼ばずにadapterを検証している。
- `cmd/secrets_test.go` に `fakeSecretStore` がある。
- `cmd/wizard_test.go` に `transactionSecretStore` がある。
- fakeごとに状態、失敗注入、呼び出し記録の実装が異なり、テスト間で挙動が重複している。

## スコープ

### 対象

- 共有in-memory SecretStoreの追加
- runtime secret解決テストの移行
- 設定ウィザードとrollbackテストの移行
- 旧形式JSON移行テストの移行
- CLI依存配線テストでのin-memory Store利用
- Keyring adapterテストとユースケーステストの責務分離
- OS資格情報ストアへアクセスしないことの回帰防止

### 対象外

- 本番の `secretstore.Store` interface変更
- 本番のKeyring backend変更
- in-memory Storeを本番fallbackとして利用する機能
- secretの永続化や暗号化をin-memory Storeへ追加すること
- 実OS Keychainを使う自動integration test
- macOS、Windows、Linux固有APIの再実装

## 配置

共有テスト基盤は次へ置く。

```text
internal/
├── secretstore/
│   ├── store.go
│   ├── keyring.go
│   └── keyring_test.go
└── testutil/
    ├── secretstore.go
    └── secretstore_test.go
```

`internal/testutil` はテストコードからだけimportする。本番コードからのimportは禁止する。`_test.go` だけに置くと別packageの `cmd` テストから共有できないため、独立したinternal packageにする。

## in-memory Store設計

### 基本構造

```go
package testutil

type SecretOperation struct {
    Method string
    Key    secretstore.Key
}

type MemorySecretStore struct {
    Values map[secretstore.Key]string
    Calls  []SecretOperation

    GetErrors    map[secretstore.Key]error
    SetErrors    map[secretstore.Key]error
    DeleteErrors map[secretstore.Key]error

    FailSetAt    int
    FailDeleteAt int
}
```

実装時はテストの必要性に合わせて最小限から始める。最初からすべてのfieldを追加せず、TDDの各シナリオで必要になった失敗注入だけを追加する。

### 通常動作

- `Get`
  - 登録済みなら値を返す。
  - 未登録なら `secretstore.ErrNotFound` を返す。
- `Set`
  - 値を登録または更新する。
- `Delete`
  - 登録済みなら削除する。
  - 未登録でも成功する。本番adapterの冪等なDeleteと揃える。
- 全操作を実行順に `Calls` へ記録する。

### 失敗注入

次の2方式を用途に応じて利用する。

1. Key単位のerror
   - resolverなど、特定secretの読み込み失敗を再現する。
2. N回目の操作で失敗
   - wizardやmigrationで、一部更新後の失敗と逆順rollbackを再現する。

注入するerrorにはsecret値を含めない。secret非漏洩テストだけは専用のsensitive markerを含むerrorを明示的に渡し、返却errorや出力から除去されることを確認する。

### 状態の所有権

- constructorへ渡された初期値はcloneする。
- `Snapshot()` を提供する場合、返却mapもcloneする。
- テスト間でmapを共有しない。
- 並列テスト対応が必要になるまではmutexを追加しない。
- 環境変数を変更するテストには引き続き `t.Parallel()` を付けない。

## テスト境界

| 対象 | 使用するtest double | 検証内容 |
|---|---|---|
| runtime resolver | MemorySecretStore | env優先、未登録、provider選択、warning |
| config wizard | MemorySecretStore | keep / set / delete、保存順序、rollback |
| legacy migration | MemorySecretStore | 既存値優先、冪等性、途中失敗、rollback |
| root/application | MemorySecretStore | Store注入、OS backendを経由しない起動 |
| Keyring adapter | keyringBackend stub | service/account変換、error正規化、値非漏洩 |
| 実OS backend | 手動smokeのみ | Set / Get / Deleteの疎通 |

Keyring adapterテストでは共有MemorySecretStoreを使わない。adapter自身を迂回してしまうため、現在の `keyringBackend` 関数stubを維持する。

## TDDテストリスト

実装時は次のリストから必ず一件だけ選び、red → green → refactorを繰り返す。

### A. MemorySecretStoreの契約

- [x] 未登録KeyのGetが `secretstore.ErrNotFound` を返す
- [x] 初期値をGetできる
- [x] Setした値をGetできる
- [x] Setで既存値を更新できる
- [x] Delete後のGetが `secretstore.ErrNotFound` を返す
- [x] 未登録KeyのDeleteが成功する
- [x] Get / Set / Deleteを実行順に記録する
- [x] constructorが初期値mapをcloneする
- [x] Snapshotが内部mapを公開しない

### B. 失敗注入

- [x] Key単位でGet errorを返せる
- [x] Key単位でSet errorを返せる
- [x] Key単位でDelete errorを返せる
- [x] N回目のSetだけを失敗させられる
- [x] N回目のDeleteだけを失敗させられる
- [x] 失敗した操作でも呼び出し履歴を確認できる
- [x] error注入後の状態が期待どおり維持される

### C. runtime resolverの移行

- [x] envがあるKeyではMemorySecretStore.Getを呼ばない
- [x] envがないKeyをin-memory値から解決する
- [x] 未登録Keyを未設定として扱う
- [x] Get失敗をsecret非包含のwarningへ変換する
- [x] AI secretがすべて未登録なら安全に失敗する
- [x] resolverがin-memory初期状態を変更しない
- [x] 既存 `fakeSecretStore` を削除する

### D. wizardの移行

- [x] 空入力で既存in-memory値を維持する
- [x] 入力値をSetする
- [x] `-` 入力でDeleteする
- [x] 2つ目のSet失敗時に1つ目を元へ戻す
- [x] Config保存失敗時に全変更を逆順で戻す
- [x] rollback失敗時も元のsecretをerrorへ含めない
- [x] cancel / EOFでStoreを変更しない
- [x] 操作履歴で保存とrollbackの順序を検証する
- [x] 既存 `transactionSecretStore` を削除する

### E. legacy migrationの移行

- [x] legacyなしではStoreへアクセスしない
- [x] 未登録Keyだけを移行する
- [x] Keychain相当の既存値を上書きしない
- [x] 同じ入力を再実行しても結果が変わらない
- [x] 途中Set失敗で今回作成したKeyだけを削除する
- [x] Config保存失敗で今回作成したKeyだけを削除する
- [x] rollback失敗を安全なerrorとして返す
- [x] 移行後のConfig JSONにsecretを含めない

### F. OS非依存性の回帰

- [x] `cmd` のテストが `NewKeyringStore()` を直接生成しない
- [x] `internal/testutil` がKeyring libraryをimportしない
- [x] production packageが `internal/testutil` をimportしない
- [x] 全自動テストがOS permission promptなしで完了する
- [x] `CGO_ENABLED=0` のテストまたはbuildが成功する
- [x] Linux向けcross buildにdesktop sessionやD-Busを要求しない

## 実装順序

### Step 1: 最小のin-memory Store

1. 未登録Getのredテストを書く。
2. `MemorySecretStore` とconstructorを追加する。
3. Get、Set、Deleteを一件ずつTDDで実装する。
4. 初期値とSnapshotのcloneを追加する。
5. `secretstore.Store` を満たすcompile-time assertionを追加する。

### Step 2: 呼び出し記録と失敗注入

1. 操作順序のredテストを書く。
2. `SecretOperation` と履歴記録を追加する。
3. Key単位のerror注入を一操作ずつ追加する。
4. rollbackテストで必要なN回目失敗を追加する。
5. test helperのAPIがシナリオ固有になりすぎていないか整理する。

### Step 3: runtime resolverテストの移行

1. `cmd/secrets_test.go` の成功系をMemorySecretStoreへ置き換える。
2. env優先時のGet未呼び出しをCallsで検証する。
3. 未登録とbackend failureを別シナリオで検証する。
4. migrationテストを移行する。
5. 利用箇所がなくなった `fakeSecretStore` を削除する。

### Step 4: wizardテストの移行

1. keep / set / deleteをMemorySecretStoreへ置き換える。
2. 一部成功後の失敗をN回目Setで再現する。
3. Config保存失敗後の逆順rollbackをCallsで検証する。
4. rollback failureとsecret非漏洩を検証する。
5. 利用箇所がなくなった `transactionSecretStore` を削除する。

### Step 5: compositionと回帰確認

1. root/applicationテストでStoreが注入可能なことを確認する。
2. テスト中に `NewKeyringStore()` が呼ばれない境界を確認する。
3. Keyring adapterテストはbackend stubのまま維持する。
4. 重複したfake、helper、assertionを整理する。
5. SPECとKeychain実装計画へin-memoryテスト方針を反映する。

## 完了条件

- `cmd` 配下のsecret関連テストが共有MemorySecretStoreを利用している。
- `fakeSecretStore` と `transactionSecretStore` の重複実装が削除されている。
- runtime resolver、wizard、migration、rollbackが実OS資格情報ストアなしで検証される。
- Keyring adapterだけがbackend stubを使ってOS境界の変換を検証している。
- 自動テストが利用者のcredential、permission dialog、D-Bus、desktop sessionへ依存しない。
- in-memory Storeが本番compositionやfallbackへ混入していない。
- error、stdout、stderr、設定ファイルへsecretが漏れない。
- 次のコマンドがすべて成功する。

```bash
gofmt -w internal/testutil/secretstore.go internal/testutil/secretstore_test.go
go test ./internal/testutil
go test ./internal/secretstore ./cmd
go test ./...
go test -race ./...
go vet ./...
CGO_ENABLED=0 go build ./...
```

## 想定される変更

- `internal/testutil/secretstore.go`: 共有MemorySecretStore、操作履歴、失敗注入
- `internal/testutil/secretstore_test.go`: in-memory Store自身の契約テスト
- `cmd/secrets_test.go`: resolver・migrationテストの移行
- `cmd/wizard_test.go`: wizard・rollbackテストの移行
- `cmd/application_test.go`: 必要に応じて共有Storeへ移行
- `cmd/root_test.go`: OS backendを経由しない依存配線の確認
- `docs/plans/feature_os_keychain.md`: テスト方針と進捗の同期
- `SPEC.md`: 自動テストでは実OS資格情報ストアを使わない旨の追記

## 手動確認

実OS資格情報ストアの疎通は自動テストから分離し、リリース前にダミー値でのみ確認する。

1. 対象OSでダミーsecretをSetする。
2. 同じ値をGetできることを確認する。
3. Delete後に未登録として扱われることを確認する。
4. OSの資格情報管理UIでテスト項目が削除済みであることを確認する。

実API keyや利用者の既存credentialは使用しない。
