# Contributing

## ビルド要件

Go 1.26.0 以上と GNU Make が必要です。
`make lint`、`make check`、`make ci` には ShellCheck も必要です。
`make ci` は Bash を備えた Linux または macOS の amd64・arm64 環境で実行し、Linux では `readelf` も必要です。

## ビルド

リポジトリのルートで次のコマンドを実行すると、`./shdox` をビルドします。

```sh
make build
```

## 開発コマンド

| コマンド | 動作 |
| --- | --- |
| `make fmt` | Go ソースを `gofmt` で整形する。 |
| `make fmt-check` | 整形が必要なファイルを表示し、失敗する。 |
| `make lint` | `go vet ./...` と ShellCheck でシェルスクリプトを検査する。 |
| `make test` | cgo を無効にしてテストを実行する。 |
| `make check` | lint、整形チェック、テストをまとめて実行する。 |
| `make build` | cgo を無効にしてビルドし、既定では `./shdox` に出力する。 |
| `make clean` | ビルド成果物を削除する。 |
| `make ci` | 整形チェック、lint、テスト、ビルド、CLI の動作確認を実行し、Linux では ELF も検査する。 |

GitHub Actions は Linux と macOS の amd64・arm64 で `make ci` を実行します。

## リリース

リリース前に `CHANGELOG.md` の変更を版付きの節にまとめ、`main` を push して CI の成功を確認します。
有効なセマンティックバージョンタグを注釈付きで作成して push すると、タグの CI 成功後に GitHub Release を作成または更新します。
以下の `X.Y.Z` はリリースするバージョンに置き換えます。

```sh
git tag -a vX.Y.Z -m "shdox vX.Y.Z"
git push origin vX.Y.Z
```

Release には Linux と macOS の amd64・arm64 向けアーカイブと `SHA256SUMS.txt` を添付します。
各アーカイブには実行ファイル、README、CHANGELOG、ライセンス、仕様書を含めます。
