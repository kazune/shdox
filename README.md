# shdox

shdox は、シェルスクリプトと関数の `##` ドキュメントコメントを検証・整形し、Markdown に変換する CLI です。

## 背景

関数の目的や使い方を自然に書けるように、概要やタグの説明文は自由記述にしています。
コメントの構造やタグの構文・出現回数・許容値を明確に定めることで、CI での形式検証と Markdown 生成に同じコメントを使えます。
シェルの位置引数には静的な型宣言がないため、`@param` は引数の位置と説明文で構成し、必要な値の形式や条件は説明文に記述できます。

## ビルド

Go 1.26.0 以上と GNU Make が必要です。
リポジトリのルートでビルドします。

```sh
make build
```

## 開発

| コマンド | 動作 |
| --- | --- |
| `make fmt` | Go ソースを `gofmt` で整形する。 |
| `make fmt-check` | 整形が必要なファイルを表示し、失敗する。 |
| `make lint` | `go vet ./...` と ShellCheck でシェルスクリプトを検査する。 |
| `make test` | cgo を無効にしてテストを実行する。 |
| `make check` | lint、整形チェック、テストをまとめて実行する。 |
| `make build` | cgo を無効にしてビルドし、既定では `./shdox` に出力する。 |
| `make clean` | ビルド成果物を削除する。 |
| `make ci` | 整形チェック、lint、テスト、ビルド、ELF 検査、CLI の動作確認を実行する。 |

`make lint`、`make check`、`make ci` には ShellCheck が必要です。
`make ci` は Bash と `readelf` を備えた Linux amd64・arm64 環境で実行します。
GitHub Actions は Linux と macOS の amd64・arm64 でテストし、Linux では追加の ELF と CLI の確認も行います。

## リリース

`v` で始まる有効なセマンティックバージョンタグを push すると、CI 成功後に GitHub Release を作成または更新します。
Release には Linux と macOS の amd64・arm64 向けアーカイブと `SHA256SUMS.txt` を添付します。
各アーカイブには実行ファイル、README、ライセンス、仕様書を含めます。

## コメントの書き方

ファイル用のドキュメントコメントは、シバン・コメント・空行のみが続くファイル冒頭に置きます。
関数用のドキュメントコメントは、関数の宣言直前に置きます。
ファイル用のドキュメントコメントは1つの連続したブロックにまとめます。
先頭のコメント空行を読み飛ばした後、次の空行までの本文が必須の概要で、複数行で書けます。
詳細説明とタグは省略でき、詳細説明を書く場合は概要の後、タグの前に置きます。
概要と詳細説明の間、および詳細説明の段落間は、`##` だけの行で区切ります。
ブロックの先頭と末尾にはコメント空行を置けます。
`fmt` は両端とタグ間のコメント空行を削除し、最初のタグの前と本文の区切りは空行1行に揃えます。
行頭またはインデントの直後から行末までを占める `##` コメントはすべて検証対象です。

以下を `example.sh` として保存します。

```bash
#!/bin/bash
## シェルスクリプトの整形を確認するための関数を定義する。

## シェルスクリプトに整形差分があるか確認する。
##
## @param $1 検査するシェルスクリプトのパス
## @return 0 整形差分なし
## @return 1 整形差分あり
## @return 2 shdox の処理エラー
## @exit 1 指定したパスが通常ファイルではない
shdox_fmt_check() {
	[ -f "$1" ] || exit 1
	shdox fmt -d "$1" >/dev/null
}
```

## 使い方

```text
Usage:
  shdox --version
  shdox check [--lang LANG] [--] FILE...
  shdox fmt [--lang LANG] [-d | -w] [--] FILE...
  shdox generate [--lang LANG] [--] FILE...
```

| コマンド | 動作 |
| --- | --- |
| `./shdox --version` | バージョンを標準出力に表示する。 |
| `./shdox check example.sh` | コメントの構文・配置・タグの値を検証する。 |
| `./shdox fmt example.sh` | 整形済みの シェルスクリプトを標準出力に書く。 |
| `./shdox fmt -d example.sh` | 整形前後の差分を標準出力に書く。 |
| `./shdox fmt -w example.sh` | 変更がある場合はファイルを上書きする。 |
| `./shdox generate example.sh` | ファイルと関数のドキュメントを1つの Markdown 文書として標準出力に書く。 |

`check`、`fmt`、`generate` は、1つ以上のファイル引数を指定順に処理します。
`--` 以降の引数はファイル名として扱います。
引数 `-` を1回指定すると標準入力を使えます。
`fmt -w` ではファイルを指定してください。
`-d` と `-w` は併用できません。

```sh
./shdox check - < example.sh
./shdox generate example.sh > functions.md
```

## シェル方言

各入力の方言は、シバン、拡張子（`.bash`・`.mksh`・`.bats`・`.zsh`）、Bash の順で決まります。
`--lang` に `bash`・`posix`・`mksh`・`bats`・`zsh` のいずれかを指定すると、標準入力を含むすべての入力にその方言が適用されます。
方言は `--lang LANG` または `--lang=LANG` の形式で指定できます。
Zsh の構文解析は実験的です。

```sh
./shdox check --lang=posix example.sh
```

## 終了コード

| コード | 意味 |
| --- | --- |
| `0` | 処理成功を表し、`fmt -d` の場合は差分なしを意味する。 |
| `1` | `check` でドキュメントコメントの仕様違反を検出したか、`fmt -d` で差分を検出した。 |
| `2` | 引数、シェル構文、文字コード、入出力などのエラー、または `fmt`・`generate` でのドキュメントコメントの仕様違反。 |

複数入力の検査で `1` と `2` の条件が重なった場合は、`2` を優先します。

タグの一覧や検証・整形の規則は [shdox 仕様](docs/shdox.md) を参照してください。
