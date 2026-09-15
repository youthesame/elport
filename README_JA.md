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
elport new "CRISPR titration"           # ノートを作成し、id 入りの report.md 雛形を生成
# ...report.md を編集し、同じ階層に fig1.png や data.csv を配置...
elport status                           # 同期予定の内容を確認（読み取り専用・サーバー送信なし）
elport push                             # 参照ファイルをアップロードし、本文を push
```

## コマンド一覧

対象ドキュメントのデフォルトは `report.md` ですが、任意のファイル名を使用できます。終了コードは成功時 `0`、失敗時 `1`
です。

| コマンド | 概要 |
|---|---|
| `elport push [<doc>]` | `<doc>` をノートに push します。ID 未設定時はノートを新規作成し、`id` をフロントマターに書き戻します。実行前にモード検証と競合チェックを行います。 |
| `elport pull [<doc>]` | リモートから本文を取得し、eLabFTW の URL をローカルパスへ逆変換した上で、参照されているファイルをダウンロードします。 |
| `elport fetch [<doc>]` | ノートに添付されているすべてのファイル（本文で参照されていないファイルを含む）をダウンロードします。読み取り専用。 |
| `elport status [<doc>]` | 副作用のない状態確認: ローカルの変更有無、リモート側の編集有無、アップロード予定ファイル、動作モードを表示します。 |
| `elport diff [<doc>]` | ソース形式での差分を表示。デフォルトはローカル ↔ リモート、`--base` 指定でローカル ↔ 直近 push 時のベース。リモートへの送信は行いません。 |
| `elport merge [<doc>]` | 競合発生後、`git merge-file` を使って `.base.md` と `.remote.md` を `<doc>` に 3 方向マージします。ローカル完結で動作し、git リポジトリ化は任意です。 |
| `elport comments [<doc>]` | リモートのコメントスレッドを表示します（ターミナル出力のみ。本文ファイルには書き込みません）。 |
| `elport comment [<doc>] "<text>"` | ノートにコメントを 1 件投稿します（編集・削除は Web UI で行ってください）。 |
| `elport new "<title>" [--entity experiments\|items] [--profile <name>] [-o <doc>]` | ノートを新規作成し、フロントマターを含むドキュメント雛形を生成します。 |
| `elport whoami [--profile <name>]` | 認証状態の確認: ユーザー情報、チームと権限ロール、API キーの read/write 権限、サーバーバージョン、スコープを表示します。 |
| `elport login [<profile>]` | `base_url` を `config.toml` に、`api_key` を OS キーリングに対話形式で保存します（キーは画面にエコーバックされません）。 |
| `elport logout [<profile>]` | 指定プロファイルの保存済み `api_key` をキーリングから削除します（`base_url` は保持されます）。 |
| `elport profile [use <name>]` | 設定済みプロファイルの一覧表示（デフォルトを明示）、またはデフォルトプロファイルを切り替えます。 |

オプション: `-n/--dry-run`（push のリハーサル。サーバーへの送信は行いません）、`--profile <name>`、
`-f/--force`（変更されたリモートを強制上書き。Web 側の変更は失われます）、`-y/--yes`（`read`/`write`
の公開範囲を自チーム外へ広げる際と、25 MiB を超えるファイルを新規アップロードする際の確認プロンプトをスキップ。
非対話シェル環境ではどちらも `-y` が必須）、`--entity {experiments,items}`。

> `pull` は参照されているファイルを**ファイル名のみ**で書き戻します。`assets/fig.png`
> のようなサブディレクトリを含むパスは `fig.png` に平坦化されます。

> `pull` がダウンロードするのは本文からリンクされているファイルのみです。生データやスペクトルデータのように、
> 本文に埋め込まれずにノートへ直接添付されているファイルはサーバー上に残ります。それらのファイルもすべて取得したい場合は
> `elport fetch` を実行してください。`fetch` は読み取り専用です。本文の解析、同期ベースの更新、次回 push
> 用マニフェストへの登録などは一切行わないため、ダウンロードしたファイルを自分で本文にリンクしない限り、次回の push
> で再アップロードされることはありません。また、ローカルに同名で内容の異なるファイルが既に存在する場合、
> 既存のローカルファイルはそのまま残し、リモートのファイルを `<ファイル名>.remote` として横に書き出します。

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

- フロントマターには `id`、人間が読むメタデータ、および任意の `profile` **のみ**を保持します。
  同期ベースやハッシュ情報は別管理の state に保存されます。本文がサーバーへ送信される前に、
  フロントマターは自動的に除去されます。
- `title` / `category` / `status` / `read` / `write` は**明示的に指定された場合のみ**リモートに反映されます。
  キーを省略した場合、elport はリモート側の既存値を変更しません。`read`/`write` は eLabFTW
  の基本公開範囲のみを設定するため、Web UI 側で個別に付与された共有設定は維持されます。
  公開範囲を自チーム外へ広げる変更を行う場合は確認を求められます（`-y` でスキップ可能。非対話シェル環境では `-y`
  が必須です）。
- フロントマターの記述と CLI の引数（profile / entity / id）に不一致がある場合、elport
  は推測せずに処理を**停止**します。

## 競合の解決

本文は eLabFTW の Web UI 側でも直接編集される可能性があるため、`push`
実行時にはまず現在のリモート内容がローカルに保存されているベース（前回同期時点のスナップショット）と比較されます:

- **変更なし** → そのまま push を続行します。
- **変更あり** → push を中断します。elport は競合解決用に `<ファイル名>.base.md`（共通の祖先）と
  `<ファイル名>.remote.md`（リモート側の最新）を書き出します。`elport merge` を実行してドキュメントに
  3 方向マージを行い、発生した競合マーカー（`<<<<<<<` / `>>>>>>>`）を手動で解消してから再度 push
  してください（競合マーカーが残った状態の本文は push が拒否されます）。
- **このマシンにベースが存在しない** → push を中断します。先に `elport pull` を実行してベースを取得するか、
  リモートの変更を破棄して上書きする場合は `--force` を指定してください。

`--force` は Web UI 側の変更を破棄するため、意図して使用してください。なお、安全網として eLabFTW
のサーバー側にはリビジョン履歴が保持されており、Web UI から過去のバージョンを復元可能です。elport
自身はローカルに独自の履歴を持ちません。編集単位でのきめ細かいバージョン管理を行いたい場合は、ファイルがプレーンな
Markdown である利点を活かし、ノートのディレクトリで `git init` を実行することをお勧めします。

## 設定と認証

**プロジェクト内に認証情報を保存しません。** `elport login` は API キーを OS キーリング（Keychain / Credential Manager /
Secret Service）に安全に保存します。base_url やその他の非機密設定は `~/.config/elport/config.toml`
に保存されます（パーミッション `600`、キーは一切出力されません）。

```toml
# ~/.config/elport/config.toml
default_profile = "labA"

[profiles.labA]
base_url   = "https://lab-a.example.org"
verify_ssl = true
```

認証情報は **環境変数 → キーリング + config → 平文設定** の優先順位で解決されます: CI 環境向けの `ELABFTW_BASE_URL` +
`ELABFTW_API_KEY`、通常はキーリングと config の組み合わせ、そしてキーリングバックエンドが存在しない環境でのみ警告付きで
config 内の平文キーにフォールバックします。プロファイル設定は VS Code の `settings.json`
のように階層構造で適用されます（`config.toml` → `<project>/.elport.toml` → `<dir>/.elport.toml`、
チームごとに 1 プロファイル）。初回 `elport login` 時に設定したプロファイルがデフォルトとなり、
`elport profile use <name>` で切り替えるか、各ノートのフロントマターで `profile:` を指定して個別に設定できます。`.elportignore` は `.gitignore`
形式で本文参照ファイルの除外パターンを指定でき、これらの設定階層をまたいで加算的に適用されます。

## さらに詳しく

- **設計思想・なぜこの仕様なのか** → [docs/DESIGN.md](docs/DESIGN.md)
- **eLabFTW API の実際の挙動と留意点** → [docs/ELABFTW-API.md](docs/ELABFTW-API.md)
- **動作仕様の正式な契約** → テストスイート（`tests/`）が唯一の正式な仕様です
- **AI エージェントから elport を操作する** → [skills/elport/SKILL.md](skills/elport/SKILL.md)

## 開発

コード構成: `client.py`（API ラッパー） / `transclude.py`（双方向の参照置換・解決） / `config.py`（設定管理） /
`state.py`（同期ベース・状態管理） / `sync.py`（push / pull / status / diff） /
`cli.py`（コマンドラインインターフェース）。**テストが動作仕様の唯一の権威であるため、
仕様を変更する際は必ずテストから先に更新してください。** 実際の API 挙動は公開デモ環境 <https://demo.elabftw.net>
で確認できます。

## 関連プロジェクト

- [elab-doc-sync](https://github.com/Kosaku-Noba/elab-doc-sync)
- [elAPI](https://github.com/uhd-urz/elAPI)

## ライセンス

MIT
