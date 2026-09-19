# elport: eLabFTW のラボノートを手元で書いて同期する git ライクな CLI

[English](README.md) | [日本語](README_JA.md)

ローカル環境でお気に入りのエディタを使い、`<figure>` などの HTML も自由に交えながら Markdown
で実験ノートを記述できます。本文と、そこで参照されているすべての図やデータファイルをまとめて eLabFTW のノートへ `push` /
`pull` して同期します。**同期の基準は常にローカルです。**

## インストール

Python 3.10 以上が必要です。`uv` ツールとしてインストールすると `elport` コマンドが利用可能になります:

```sh
uv tool install git+https://github.com/youthesame/elport
```

## コンセプト

ローカルの 1 ファイルが eLabFTW 上の 1 つのノートに対応します。YAML フロントマターを持つ Markdown ファイルとして記述し、
インライン HTML も自由に使用できます。

`push` を実行すると、elport は記法（Markdown 画像記法、HTML タグ、通常リンクなど）を問わず、
本文内で参照されている実在のローカルファイルをすべてアップロードし、各パスを正規の eLabFTW URL へと自動置換します。
本文は生の Markdown として送信されるため、`<figure>` や数式（`$...$`）などもそのまま意図通りにレンダリングされます。
別のノートにリンクしたい場合は、その eLabFTW URL をそのまま書くだけです。コードブロック、インラインコード、HTML
コメントの内部は一切解析・置換されません。

## クイックスタート

```sh
elport login labA                       # base_url（config）と api_key（OS キーリング）を対話形式で保存
elport list                             # サーバー上にある自分のノートを確認（読み取り専用）
elport new "CRISPR titration"           # ノートを作成し、id 入りの report.md 雛形を生成
# ...Web UI で作成済みのノートから始めるなら: elport clone <id>
# ...report.md を編集し、同じ階層に fig1.png や data.csv を配置...
elport status                           # 同期予定の内容を確認（読み取り専用・サーバー送信なし）
elport push                             # 参照ファイルをアップロードし、本文を push
```

## コマンド一覧

ローカルドキュメントを対象とするコマンドは、省略可能な `[<doc>]` を取ります。デフォルトは `report.md`
ですが、任意のファイル名を使用できます。終了コードは成功時 `0`、失敗時 `1` です。オプションの詳細は
`elport <command> --help` で確認できます。

### 同期

| コマンド | 概要 |
|---|---|
| `push` | 本文と参照ファイルをノートへアップロードします。 |
| `pull` | 本文とメタデータを取得し、参照ファイルをダウンロードします。 |
| `fetch` | 参照の有無にかかわらず、添付ファイルをすべてダウンロードします。読み取り専用。 |
| `status` | 同期予定の内容と動作モードを表示します。読み取り専用。 |
| `diff` | ローカルとリモートの差分。`--base` で直近 push 時との差分。 |
| `merge` | 競合後、`.base.md` と `.remote.md` を `<doc>` に 3 方向マージします。 |

`push` は `id` が未設定の場合にノートを新規作成し、`id` をフロントマターへ書き戻します。実行前にモード検証と
競合チェックを行います。`merge` はローカル完結で `git merge-file` を呼び出すため、git リポジトリ化は任意です。

### ノート操作

| コマンド | 概要 |
|---|---|
| `new "<title>"` | ノートを新規作成し、フロントマターの雛形を生成します。 |
| `clone <id>` | 既存ノートからドキュメントを起こし、pull します。 |
| `list` | リモートのノートを `id  日付  タイトル` 形式で一覧表示します。読み取り専用。 |
| `view <id>` | リモートの本文を保存されたままの形で標準出力へ表示します。 |
| `comments` | リモートのコメントスレッドを表示します。 |
| `comment "<text>"` | コメントを 1 件投稿します。編集・削除は Web UI で行ってください。 |

`new` と `clone` は `-o <doc>` を指定しない限り `report.md` に書き出します。`list`
はデフォルトで自分のノートのみ（`--scope self`）を 50 件ずつ返し、`-q <term>`、`--limit`、`--offset`、`--json`
で絞り込めます。`comments` はターミナルに出力するだけで、本文ファイルには書き込みません。

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

### pull と fetch の違い

この 2 つはダウンロードする対象が異なり、どちらか一方で用が足りるわけではありません。

- `pull` は参照ファイルを**ファイル名のみ**で書き戻します。`assets/fig.png`
  のようなサブディレクトリを含むパスは `fig.png` に平坦化されます。
- `pull` がダウンロードするのは本文からリンクされているファイルだけです。生データやスペクトルのように、
  本文に埋め込まれずノートへ直接添付されているファイルはサーバー上に残ります。
- `fetch` はそれらを取得します。本文の解析、同期ベースの更新、push
  マニフェストへの登録は一切行わないため、自分で本文にリンクしない限り再アップロードされることはありません。
- ローカルに同名で内容の異なるファイルがある場合、`fetch` は既存ファイルをそのまま残し、
  リモート側を `<ファイル名>.remote` として横に書き出します。

## ドキュメント形式（フロントマター）

ファイルの先頭に YAML ブロックを記述します（存在しない場合は push 時に自動生成・補完されます）:

```markdown
---
id: 42                        # ノート ID（新規作成時に自動補完）
entity: experiments           # experiments | items（デフォルト: experiments）
title: "experiment title"     # 任意。未指定時は作成時にファイル名が設定される
tags: [CRISPR, PCR]           # 任意。追加専用（タグの削除は Web UI で実行）
category: Molecular Biology   # 任意。ID または既存のカテゴリ名（elport は新規カテゴリを作成しません）
status: Running               # 任意。ID または既存のステータス名（elport は新規ステータスを作成しません）
profile: labA                 # 任意。送信先プロファイル
read: team                    # 任意。owner | owner+admin | team | account | public
write: owner                  # 任意。上記と同じ権限レベル
---

# 本文。Markdown で記述し、インライン HTML も使用可能 ...
```

フロントマターが保持するのは `id`、人間が読むメタデータ、任意の `profile` **のみ**です。
同期ベースやハッシュ情報は別管理の state に保存されます。本文の送信前に、フロントマターは自動的に除去されます。

`title` / `category` / `status` / `read` / `write` は**明示的に指定された場合のみ**リモートに反映されます。
キーを省略した場合、elport はリモート側の既存値を変更しません。pull はこれらをサーバーの値で書き戻すため、
自分で書いていないキーが pull 後に現れることがあります。ローカルで変更してまだ push
していないキーはそのまま残ります。`tags` は双方向で追加のみのため、pull では和集合になります。

`read`/`write` は eLabFTW の基本公開範囲のみを設定するため、Web UI
側で個別に付与された共有設定は維持されます。公開範囲を自チーム外へ広げる場合は確認を求められます。

フロントマターの記述と CLI の引数（profile / entity / id）に不一致がある場合、elport
は推測せずに処理を**停止**します。

## 競合の解決

本文は eLabFTW の Web UI 側でも直接編集される可能性があるため、`push`
実行時にはまず現在のリモート内容がローカルに保存されているベース（前回同期時点のスナップショット）と比較されます。

- **変更なし** → そのまま push を続行します。
- **変更あり** → push を中断します。elport は競合解決用に `<ファイル名>.base.md`（共通の祖先）と
  `<ファイル名>.remote.md`（リモート側の最新）を書き出します。`elport merge` で 3 方向マージを行い、
  発生した競合マーカー（`<<<<<<<` / `>>>>>>>`）を手動で解消してから再度 push
  してください。競合マーカーが残った本文は push が拒否されます。
- **このマシンにベースが存在しない** → push を中断します。先に `elport pull` を実行するか、
  リモートの変更を破棄して上書きする場合は `--force` を指定してください。

`--force` は Web UI 側の変更を破棄するため、意図して使用してください。安全網として eLabFTW
のサーバー側にはリビジョン履歴が保持されており、Web UI から過去のバージョンを復元できます。ただし elport
自身はローカルに独自の履歴を持ちません。編集単位のバージョン管理を行いたい場合は、ファイルがプレーンな Markdown
である利点を活かし、ノートのディレクトリで `git init` してください。

## 設定と認証

**プロジェクト内に認証情報を保存しません。**

### キーの保存場所

`elport login` は API キーを OS キーリング（Keychain / Credential Manager / Secret Service）に保存します。
base_url を含む非機密設定は `~/.config/elport/config.toml`（パーミッション `600`）に保存されます。
キーが画面に出力されることはありません。

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
3. config 内の平文キー。キーリングバックエンドが存在しない環境でのみ、警告付きで使われる最終手段です。

### プロファイル

プロファイル設定は VS Code の `settings.json` のように階層構造で適用されます（チームごとに 1 プロファイル）。

```
~/.config/elport/config.toml  →  <project>/.elport.toml  →  <dir>/.elport.toml
```

初回 `elport login` 時に設定したプロファイルがデフォルトになります。`elport profile use <name>`
で切り替えるか、各ノートのフロントマターで `profile:` を指定して個別に固定できます。

### .elportignore

`.elportignore` は `.gitignore` 形式で、本文から参照されているファイルをアップロード対象から除外します。
プロファイルと同じ設定階層に加えて、プロジェクトルートからドキュメントのあるディレクトリまでのすべての
`.elportignore` が加算的に適用されます。各パターンは、それが置かれたディレクトリを基準に解釈されます。

## さらに詳しく

- **設計思想・なぜこの仕様なのか** → [docs/DESIGN.md](docs/DESIGN.md)
- **eLabFTW API の実際の挙動と留意点** → [docs/ELABFTW-API.md](docs/ELABFTW-API.md)
- **動作仕様の正式な契約** → テストスイート（`tests/`）が唯一の正式な仕様です
- **AI エージェントから elport を操作する** → [skills/elport/SKILL.md](skills/elport/SKILL.md)

## 開発

| モジュール | 役割 |
|---|---|
| `cli.py` | 引数の解析とディスパッチ |
| `sync.py` | push / pull / status / diff |
| `transclude.py` | パス ↔ URL の相互変換 |
| `client.py` | eLabFTW API ラッパー |
| `state.py` | 同期ベースとハッシュの管理 |
| `config.py` | プロファイルと認証情報の解決 |
| `frontmatter.py` | YAML ブロックの処理 |
| `browse.py` | list と view |

**テストが動作仕様の唯一の権威であるため、仕様を変更する際は必ずテストから先に更新してください。** 実際の API
挙動は公開デモ環境 <https://demo.elabftw.net> で確認できます。

## 謝辞

- [elab-doc-sync](https://github.com/Kosaku-Noba/elab-doc-sync)
- [elAPI](https://github.com/uhd-urz/elAPI)

## ライセンス

MIT
