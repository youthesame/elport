# elport: eLabFTW のラボノートを手元で書いて同期する git ライクな CLI

[English](README.md) | [日本語](README_JA.md)

ローカル環境でお気に入りのエディタを使い、`<figure>` などの HTML も自由に交えながら Markdown
で実験ノートを記述できます。本文と、そこで参照されているすべての図やデータファイルをまとめて eLabFTW のノートへ `push` /
`pull` して同期します。**同期の基準は常にローカルです。**

## インストール

Python 3.10 以上が必要です。`uv` ツールとしてインストールすると `elport` コマンドが利用可能になります:

```sh
uv tool install elport    # 更新: uv tool upgrade elport
```

pip でもインストールできます:

```sh
pip install elport        # 更新: pip install -U elport
```

## コンセプト

ローカルの 1 ファイルが eLabFTW 上の 1 つのノートに対応します。YAML フロントマターを持つ Markdown ファイルとして記述し、
インライン HTML も自由に使用できます。

`push` を実行すると、elport は記法（Markdown 画像記法、HTML タグ、通常リンクなど）を問わず、
本文内で参照されている実在のローカルファイルを場所を問わず（`../data/raw.csv` や絶対パスも可。隠しファイルは除外）すべてアップロードし、各パスを正規の eLabFTW URL へと自動置換します。
本文は生の Markdown として送信されるため、`<figure>` や数式（`$...$`）などもそのまま意図通りにレンダリングされます。
別のノートにリンクしたい場合は、その eLabFTW URL かローカルのドキュメントのパスを書くだけです。コードブロック、インラインコード、HTML
コメントの内部は一切解析・置換されません。

## クイックスタート

```sh
elport login labA                       # base_url（config）と api_key（OS キーリング）を対話形式で保存
elport list                             # サーバー上にある自分のノートを確認（読み取り専用）
elport new "CRISPR titration"           # ノートを作成し、id 入りの report.md 雛形を生成
# ...Web UI で作成済みのノートを編集するなら: elport clone <id>（読むだけなら: elport view <id>）
# ...report.md を編集し、同じ階層に fig1.png や data.csv を配置...
elport status                           # 同期予定の内容を確認（読み取り専用・サーバー送信なし）
elport push                             # 参照ファイルをアップロードし、本文を push
```

## ノートの書き方

```markdown
---
id: 42                    # elport new か最初の push が書き込む
title: CRISPR titration
tags: [CRISPR]
---

# Result

![titration curve](fig1.png)

Raw data: [raw.csv](data/raw.csv)
```

フロントマターは `elport new` が書きます。本文はふつうに書き、ローカルのファイルをパスで参照すれば push がアップロードします。
フロントマターのすべてのキーと参照のルールは `elport help documents` で確認できます。

## コマンド一覧

ローカルドキュメントを対象とするコマンドは、省略可能な `[<doc>]` を取ります。デフォルトは `report.md`
ですが、任意のファイル名を使用できます。同期コマンドには
ディレクトリも渡せ、その下の elport ドキュメントを 1 行ずつ処理します。終了コードは成功時 `0`、失敗時 `1` です。オプションの詳細は
`elport <command> --help` で確認できます。

### 同期

| コマンド | 概要 |
|---|---|
| `push` | 本文と参照ファイルをノートへアップロードします。 |
| `pull` | 本文とメタデータを取得し、参照ファイルをダウンロードします。 |
| `fetch` | 参照の有無にかかわらず、添付ファイルをすべてダウンロードします。読み取り専用。 |
| `status` | 同期予定の内容と動作モードを表示します。読み取り専用。 |
| `diff` | ローカルとリモートの差分。`--base` で直近 push 時との差分。 |
| `merge` | 競合後、`.base.md` と `.remote.md` を `<doc>` に 3 方向マージします。`git` が必要です。 |

`push` は `id` が未設定の場合にノートを新規作成し、`id` をフロントマターへ書き戻します。実行前にモード検証と
競合チェックを行います。`merge` はローカル完結で `git merge-file` を呼び出すため `git` のインストールが必要ですが、
フォルダを git リポジトリにする必要はありません。

### ノート操作

| コマンド | 概要 |
|---|---|
| `new "<title>"` | ノートを新規作成し、フロントマターの雛形を生成します。 |
| `clone <id>` | 既存ノートを編集して push するためのドキュメントを起こし、pull します。読むだけなら `view` を使います。 |
| `clone --all <dir>` | すべてのノートを `<dir>` にバックアップします。再実行で新しいノートを追加します。HTML のノートは `view -o` と同じ形で保存します。 |
| `list` | リモートのノートを `id  日付  タイトル` 形式で一覧表示します。読み取り専用。 |
| `view <id>` | リモートのノートを読みます。本文を読みやすい形にして標準出力へ表示します。読み取り専用です。HTML のノートは書式を除き、添付はファイル名で示します。このノートからのリンクと、このノートへのリンクを標準エラーに一覧します。 |
| `view <id> -o <dir>` | 本文とすべての添付ファイルを、新規または空の `<dir>` に書き出して読みます。作業コピーにはなりません。 |
| `activity` | 自分が作成したノートを日ごとのヒートマップで表示します（端末幅に収まる範囲で最大 1 年）。`--team` で著者ごとに表示します。 |
| `comments [<id>]` | リモートのコメントスレッドを表示します。`<id>` を渡せばローカル文書は不要です。 |
| `comment "<text>"` | コメントを 1 件投稿します。編集・削除は Web UI で行ってください。 |

`new` と `clone` は `-o <doc>` を指定しない限り `report.md` に書き出します。`list`
はデフォルトで自分のノートのみ（`--scope self`）を 50 件ずつ返し、`-q <term>`、`--limit`、`--offset`、`--json`
で絞り込めます。`--all` を付けると全件をページ送りで取得します。`comments` はターミナルに出力するだけで、本文ファイルには書き込みません。

Web UI のリッチテキストエディタで書いたノートは、本文が HTML で保存されます。elport はこれを読むだけです。`clone`、`pull`、
`push` は拒否するので、図も含めて読むときは `view -o` を使ってください。

### セットアップ

| コマンド | 概要 |
|---|---|
| `login [<profile>]` | `base_url` を `config.toml` に、`api_key` を OS キーリングに保存します。 |
| `logout [<profile>]` | 保存済みの `api_key` を削除します。`base_url` は保持されます。 |
| `profile [use <name>]` | プロファイルを一覧表示、またはデフォルトを切り替えます。 |
| `whoami` | 認証状態の確認: ユーザー、チームと権限、キーの権限、サーバーバージョン、スコープ。 |

### 共通オプション

- `-n/--dry-run` は push のリハーサルを行い、サーバーへは送信しません。
- `-f/--force` は変更されたリモートを強制上書きします。Web 側の変更は失われます。
- `-y/--yes` は 2 つの確認をスキップします。`read`/`write` の公開範囲を自チーム外へ広げる場合と、25 MiB
  を超えるファイルを新規アップロードする場合です。非対話シェル環境ではどちらも `-y` が必須です。
- `--profile <name>` と `--entity {experiments,items}` で送信先を指定します。

## ドキュメント、同期、競合

詳しい説明は CLI に同梱しているので、インストールした版と必ず一致します（英語）。

- `elport help documents`: フロントマター、参照、添付、ノート間のリンク（[GitHub で読む](https://github.com/youthesame/elport/blob/main/src/elport/help/documents.md)）
- `elport help workflow`: status、diff、push の流れ、競合、pull と fetch（[GitHub で読む](https://github.com/youthesame/elport/blob/main/src/elport/help/workflow.md)）
- `elport help agents`: AI エージェントが実行前にユーザーへ確認すべきこと（[GitHub で読む](https://github.com/youthesame/elport/blob/main/src/elport/help/agents.md)）

## 設定と認証

**プロジェクト内に認証情報を保存しません。**

### キーの保存場所

`elport login` は API キーを OS キーリング（Keychain / Credential Manager / Secret Service）に保存します。
base_url を含む非機密設定は `~/.config/elport/config.toml`（パーミッション `600`）に保存されます。
キーが画面に出力されることはなく、`https://` 以外の base_url は拒否されます。

```toml
# ~/.config/elport/config.toml
default_profile = "labA"

[profiles.labA]
base_url   = "https://lab-a.example.org"
verify_ssl = true
```

### 認証情報の優先順位

elport は次の 3 つを順に確認し、最初に見つかったものを使用します。

1. 環境変数 `ELABFTW_BASE_URL` + `ELABFTW_API_KEY`。CI 環境ではこれを使います。
2. `elport login` が保存したキーリングと config の組み合わせ。通常はこれがデフォルトです。
3. `~/.config/elport/config.toml` 内の平文キー。キーリングバックエンドが存在しない環境（WSL、ヘッドレスサーバーなど）でのみ、警告付きで使われる最終手段です。

### プロファイル

プロファイル設定は VS Code の `settings.json` のように階層構造で適用されます（チームごとに 1 プロファイル）。

```
~/.config/elport/config.toml  →  <project>/.elport.toml  →  <dir>/.elport.toml
```

初回 `elport login` 時に設定したプロファイルがデフォルトになります。`elport profile use <name>`
で切り替えるか、各ノートのフロントマターで `profile:` を指定して個別に固定できます。

### .elportignore

`.elportignore` は `.gitignore` 形式で、ドキュメントのディレクトリ内にある参照ファイルをアップロード対象から除外します。
プロファイルと同じ設定階層に加えて、プロジェクトルートからドキュメントのあるディレクトリまでのすべての
`.elportignore` が加算的に適用されます。各パターンは、それが置かれたディレクトリを基準に解釈されます。

## さらに詳しく

- **設計思想・なぜこの仕様なのか** → [docs/DESIGN.md](docs/DESIGN.md)
- **eLabFTW API の実際の挙動と留意点** → [docs/ELABFTW-API.md](docs/ELABFTW-API.md)
- **動作仕様の正式な契約** → テストスイート（`tests/`）が唯一の正式な仕様です
- **AI エージェントから elport を操作する** → `elport help agents`。[skills/elport/SKILL.md](skills/elport/SKILL.md) の skill はそこを案内するだけです

## 開発

| モジュール | 役割 |
|---|---|
| `cli.py` | 引数の解析とディスパッチ |
| `sync.py` | push / pull / status / diff / view |
| `transclude.py` | パス ↔ URL の相互変換 |
| `client.py` | eLabFTW API ラッパー |
| `state.py` | 同期ベースとハッシュの管理 |
| `config.py` | プロファイルと認証情報の解決 |
| `frontmatter.py` | YAML ブロックの処理 |
| `browse.py` | list、activity |
| `help/*.md` | `elport help` が表示するトピック |

**テストが動作仕様の唯一の権威であるため、仕様を変更する際は必ずテストから先に更新してください。** 実際の API
挙動は公開デモ環境 <https://demo.elabftw.net> で確認できます。

## 謝辞

- [elab-doc-sync](https://github.com/Kosaku-Noba/elab-doc-sync)
- [elAPI](https://github.com/uhd-urz/elAPI)

## ライセンス

MIT
