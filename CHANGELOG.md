# Changelog

## [Unreleased]

### 追加

- `@return` と `@exit` で、個別に列挙していないステータスを `other` で記述できるようにした。

## [0.2.0] - 2026-11-28

### 追加

- 実行環境から受け取る変数を記述する `@env` と、同じシェル内で共有する変数を記述する `@global` に対応。

### 変更

- 生成する Markdown の関数セクションを、詳細にリンクする一覧表と関数詳細に分割。

## [0.1.0] - 2026-11-27

### 追加

- シェルのドキュメントコメントを検証・整形し、Markdown を生成する `shdox` CLI を追加。
- Bash、POSIX sh、mksh、Bats、実験的な Zsh の構文解析に対応。
- unified diff の出力と、Linux・macOS での `fmt -w` のアトミックなファイル置換に対応。
- Linux と macOS の amd64・arm64 向けアーカイブを SHA-256 チェックサムとともに GitHub Release へ公開。
