# Plan: CLI wiring

## 目的

TUI の公開境界を Cobra CLI と `main.go` へ配線し、設定・保存先・外部クライアントを実行時に組み立てられるようにする。

## テストリスト

- [x] ルートコマンドが `run` / `config` / `version` / `where` を公開する
- [x] 引数なしと `run` が同じ TUI 起動処理を呼ぶ
- [x] `run` が設定、データストア、GitHub、Claude、OpenAI を組み立てる
- [x] `run` が設定読込エラーと TUI 起動エラーを返す
- [x] `where` が設定ファイルとデータファイルのパスを表示する
- [x] XDG_DATA_HOME があればデータファイルの解決に優先する
- [x] XDG_DATA_HOME がなければユーザーホーム配下へフォールバックする
- [x] `config` が既存値を既定値として対話入力し、保存する
- [x] `config` の空入力は既存値を維持し、provider/language の不正値は再入力させる
- [x] `config` は API キーを出力へ露出しない
- [x] `version` がバージョンを表示する
- [x] `main.go` が CLI の終了コードをプロセスへ反映できる構造にする
- [x] 全テスト・race test・vet・build が成功する

## 実装方針

- `cmd` 内の副作用（設定読込・保存、TUI 起動、ホームディレクトリ解決）を関数として注入し、ネットワークや実端末なしで配線をテストする。
- Cobra コマンドはテストごとに新規生成し、グローバル状態を持たせない。
- データ保存先は `$XDG_DATA_HOME/reporepo/data.json`、未設定時は `~/.local/share/reporepo/data.json` とする。
- モデル既定値は既存仕様に合わせ、Claude は `claude-sonnet-4-6`、OpenAI は `gpt-4o-mini` とする。
