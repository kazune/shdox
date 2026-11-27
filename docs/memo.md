形式検証（バリデーション）に耐える仕様にするなら、タグの意味だけでなく、**構文・出現回数・配置・許容値を厳密に定義する**必要があります。

最小限の仕様として、以下の設計が考えられます。

## **1. 基本形式**

```
## ファイルを検証する。
##
## ファイルの存在を確認する。
## 存在しない場合はシェルを終了する。
##
## 空ファイルは検証失敗として扱う。
##
## @param $1 ファイルパス
## @return 0 検証成功
## @return 1 空ファイル
## @exit 2 ファイルが存在しない
validate_file() {
    [[ -f "$1" ]] || exit 2
    [[ -s "$1" ]] || return 1
    return 0
}
```

概要、詳細説明、タグの順に記述します。
詳細説明とタグは省略できます。

## **2. タグの構文**

| タグ            | 構文                                | 出現回数 |
| ------------- | --------------------------------- | ---- |
| `@param`      | `@param <position> <description>` | 0～複数 |
| `@return`     | `@return <status> <description>`  | 0～複数 |
| `@exit`       | `@exit <status> <description>`    | 0～複数 |
| `@stdin`      | `@stdin <description>`            | 0～1  |
| `@stdout`     | `@stdout <description>`           | 0～1  |
| `@stderr`     | `@stderr <description>`           | 0～1  |
| `@sideeffect` | `@sideeffect <description>`       | 0～複数 |

Bash は静的型付け言語ではないため、最小仕様では `@param` に型フィールドを設けないものとします。

## **3. 文法定義（EBNF）**

```
document   = summary, [ blank, details ],
             [ blank, tags ];

summary    = text-line;
details    = paragraph, { blank, paragraph };
paragraph  = text-line, { text-line };
tags       = tag, { tag };

tag        = "##", separator, (
               param
             | return
             | exit
             | stdin
             | stdout
             | stderr
             | sideeffect
             ), newline;

param      = "@param", separator, position, separator, description;
return     = "@return", separator, status, separator, description;
exit       = "@exit", separator, status, separator, description;
stdin      = "@stdin", separator, description;
stdout     = "@stdout", separator, description;
stderr     = "@stderr", separator, description;
sideeffect = "@sideeffect", separator, description;

position   = "$", nonzero, { digit };
status     = "0" | nonzero, { digit };

blank      = "##", { hspace }, newline;
text-line  = "##", separator, description, newline;
separator  = hspace, { hspace };
hspace     = ? ASCII スペースまたは水平タブ ?;

nonzero    = "1" | "2" | "3" | "4" | "5"
           | "6" | "7" | "8" | "9";

digit      = "0" | nonzero;
```

概要は1行とし、詳細説明には複数の段落を許容します。
セクション間と詳細説明の段落間には、空行を厳密に1行入れます。
タグ間とドキュメント末尾に空行は置けません。

水平空白は ASCII スペースと水平タブです。
解析時に行末の水平空白を除去し、`##` に水平空白だけが続く行は空行として扱います。
`##` と本文の間、タグ名と後続の引数または説明文の間、引数と説明文の間には、水平空白が1文字以上必要です。
説明文は改行を含まない非空文字列で、内部の空白は自由に記述できます。
`##` 直後の水平空白をすべて読み飛ばしたとき、最初の文字が `@` ならタグ行として解析します。

## **4. 検証規則**

| 検証項目                | 規則                 |
| ------------------- | ------------------ |
| コメント接頭辞             | `##` の後に水平空白1文字以上、または空行 |
| 概要                  | 必須・非空の1行          |
| 詳細説明                | 省略可能               |
| タグ名                 | 定義済みタグのみ許可         |
| 位置引数                | `$1` 以上の正整数        |
| 終了ステータス             | 0～255 の整数          |
| 説明文                 | 必須・非空              |
| 引数の重複               | 禁止                 |
| `@return` 内のステータス重複 | 禁止                 |
| `@exit` 内のステータス重複   | 禁止                 |
| 単一出現タグの重複           | 禁止                 |
| タグ間の順序              | 任意                 |
| タグ以降の自由記述           | 禁止                 |
| 関数との間の空行            | 禁止                 |

`@return 1` と `@exit 1` の併存は、意味が異なるため許可します。

## **5. バリデーションとフォーマット**

| コマンド | 規則 |
| --- | --- |
| `shdox check` | 文法・構造・許容値を検証し、ASCII スペースと水平タブの混在を許容する |
| `shdox fmt` | 構文上の区切りを ASCII スペース1文字に統一する |
| `shdox generate` | ドキュメントを生成し、入力では ASCII スペースと水平タブの混在を許容する |

## **6. バリデーションの例**

**有効**

```
## 処理を実行する。
##
## @param $1 ファイルパス
## @return 0 正常終了
## @return 1 処理失敗
## @exit 1 致命的エラー
run_task() {
    return 0
}
```

**無効**

```
## 処理を実行する。
##
## @param $0 ファイルパス
## @return -1 異常終了
## @return 256 異常終了
## @exit 1
## @unknown 未定義タグ
```

不正な引数番号、範囲外のステータス、説明文の欠落、未定義タグを検出する。

次のコメントブロックでは、概要と詳細説明の間に空行が連続しています。

```sh
## 処理を実行する。
##
##
## 処理の詳細を説明する。
```

次のコメントブロックにはタグ間に空行があります。

```sh
## 処理を実行する。
##
## @return 0 正常終了
##
## @return 1 処理失敗
```

次のコメントブロックは空行で終わっています。

```sh
## 処理を実行する。
##
```

## **7. 検証レベルの分離**

検証は、以下の3段階に分離するのが適切です。

**Level 1 — Lexical validation（字句検証）**

コメント接頭辞、タグ名、文字列形式の検証。

**Level 2 — Structural validation（構造検証）**

必須項目、出現回数、重複、許容値、配置の検証。

**Level 3 — Semantic validation（意味検証）**

関数の実装とドキュメントの整合性の検証。
Bash の構文解析や制御フロー解析が必要。

最初は Level 1・2 のみで十分です。
Level 3 では、暗黙的な関数終了ステータスや、サブシェル内の `exit`、`set -e` なども考慮する必要があり、実装コストが大幅に増加します。

**設計方針としては、人間向けの説明文は自由記述、機械向けの構造は厳密に定義する**のがよいと思います。
これなら記述負担を抑えながら、CI による形式検証や Markdown 生成にも対応できます。
