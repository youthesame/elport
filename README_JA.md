# elport — eLabFTW のラボノートを手元で書いて同期する git ライクな CLI

[English](README.md) | [日本語](README_JA.md)

ラボノートを手元のエディタで Markdown として書き、`<figure>` などの HTML 断片も混ぜられます。図やデータファイルごと
eLabFTW へ `push` / `pull` します。同期の基準になるのはいつも手元のファイルです。

## インストール

Python 3.10 以上が必要です。`uv` ツールとして入れると `elport` コマンドが使えます。

```sh
uv tool install git+https://github.com/youthesame/elport
```

## 中心となる考え方

手元の 1 ファイルが eLabFTW 上の 1 件の記録に対応します。ファイルは YAML フロントマターを付けた Markdown で、`<figure>`
などの HTML 断片も書けます。`push` すると、elport は本文が参照している実在のローカルファイルを記法によらずアップロードし、
そのパスを本物の eLabFTW の URL に置き換えます。本文は生の Markdown として送られるので `<figure>` や `$...$` はそのまま
表示されます。別の記録は、その eLabFTW の URL をそのまま書けばアップロードせずリンクできます。コードフェンス・インライン
コード・HTML コメントの中は解析しません。

## クイックスタート

```sh
elport login labA                       # base_url を config に、api_key を OS キーリングに保存。対話式
elport new "260809 CRISPR titration"    # 記録を作成し、id 付きの report.md を雛形生成
# ...report.md を編集し、fig1.png や data.csv を隣に置く...
elport status                           # 何が同期されるか確認。送信はしない
elport push                             # 参照ファイルをアップロードして本文を push
```

## コマンド

デフォルトのドキュメントは `report.md` です。名前は自由に付けられます。成功で終了コード `0`、失敗で `1` を返します。

| コマンド | 概要 |
|---|---|
| `elport push [<doc>]` | `<doc>` を 1 件の記録に push します。記録が無ければ作成し `id` を書き戻します。先にモードとコンフリクトを確認します。 |
| `elport pull [<doc>]` | 本文を取得し、URL をローカルパスへ戻し、参照ファイルをダウンロードします。 |
| `elport status [<doc>]` | 副作用なし。ローカルの変更、リモートの編集、アップロードされるファイル、モードを表示します。 |
| `elport diff [<doc>]` | ソース形式の差分。デフォルトはローカルとリモートの比較で、`--base` を付けるとローカルと最後の push の比較になります。送信しません。 |
| `elport merge [<doc>]` | コンフリクト後、`.base.md`/`.remote.md` を `git merge-file` で `<doc>` に3-wayマージします。ローカルのみ・git は任意。 |
| `elport comments [<doc>]` | リモートのコメントスレッドを表示します（端末のみ。本文には書き込みません）。 |
| `elport comment [<doc>] "<text>"` | コメントを1件投稿します（編集・削除はしません。Web UI で行ってください）。 |
| `elport new "<title>" [--entity experiments\|items] [--profile <name>] [-o <doc>]` | 記録を作成し、フロントマターの雛形を生成します。 |
| `elport whoami [--profile <name>]` | 認証確認。ユーザー・チームと権限・API キーの read/write・サーバーバージョン・スコープを表示します。 |
| `elport login [<profile>]` | `base_url` を `config.toml` に、`api_key` を OS キーリングに保存します。入力はプロンプト式で、キーは画面に出ません。 |
| `elport logout [<profile>]` | プロファイルの保存済み `api_key` を削除し、`base_url` は残します。 |
| `elport profile [use <name>]` | プロファイル一覧（既定を明示）、または既定プロファイルを設定します。 |

オプション: `-n`/`--dry-run`（push のリハーサル。送信しない）、`--profile <name>`、`-f`/`--force`（変更済みのリモートに
上書き push。Web 側の変更は失われる）、`-y`/`--yes`（`read`/`write` をチーム外へ広げるときの確認を省略）、
`--entity {experiments,items}`。

> pull でファイルを書き戻すときは**ベース名だけ**を使います。`assets/fig.png` のようなサブディレクトリ付きのパスは
> `fig.png` に平坦化されます。

## ドキュメント形式 — フロントマター

先頭に YAML ブロックを置きます。無ければ push 時に生成・補完されます。

```markdown
---
id: 42                   # 記録の ID。作成時に自動で埋まる
entity: experiments           # experiments か items。デフォルトは experiments
title: "experiment title"
tags: [CRISPR, PCR]           # 任意。追加のみ（削除は Web UI で）
category: Molecular Biology   # 任意。ID または既存のカテゴリ名（elport は作成しない）
status: Running               # 任意。ID または既存のステータス名（elport は作成しない）
profile: labA                 # 任意。送信先プロファイル
read: team                    # 任意。owner | owner+admin | team | account | public
write: owner                  # 任意。同じ段階
---

# 本文。Markdown で書き、HTML 断片も混ぜてよい ...
```

- 保持するのは `id` と人が読むメタデータ、任意の `profile` だけです。ベースやハッシュは state に持ちます。フロント
  マターは本文を送る前に取り除かれます。
- `title` / `category` / `status` / `read` / `write` は**書いたときだけ**反映され、省略した項目はリモートの値に触れません。
  `read`/`write` はベース公開範囲のみを設定するので Web UI の個別共有は保持され、チーム外へ広げるときは確認します
  （`-y` で省略。非対話では `-y` が必要）。
- フロントマターと CLI が profile や entity、id で食い違ったら、elport は推測せずに**停止**します。

## コンフリクト

本文は Web UI でも編集できるため、`push` はまず現在のリモートを保存済みのベースと比べます。

- **変更なし** … そのまま続行します。
- **変更あり** … 中止します。elport は `<name>.base.md`（祖先）と `<name>.remote.md` を出力するので、`elport merge` で
  `<doc>` に3-wayマージし、`<<<<<<<`/`>>>>>>>` マーカーを解消してから push します（マーカーが残った本文は push が拒否
  します）。
- **このマシンにベースが無い** … 中止します。先に `elport pull` するか、`--force` で上書きします。

`--force` は Web 側の変更を捨てます。意図して使ってください。eLabFTW は Web UI から復元できるサーバー側の履歴を安全網
として持っています。elport 自身は手元の履歴を持ちません。編集ごとの履歴が欲しければノートフォルダで `git init` して
ください。ファイルはただの Markdown です。

## 設定と認証

**資格情報はプロジェクトの中に置きません。** `elport login` で API キーを OS キーリング（Keychain / Credential Manager /
Secret Service）に保存し、base_url など機密でない値は `~/.config/elport/config.toml`（モード `600`、キーは非表示）に置き
ます。

```toml
# ~/.config/elport/config.toml
default_profile = "labA"

[profiles.labA]
base_url   = "https://lab-a.example.org"
verify_ssl = true
```

資格情報の解決順は **環境変数 → キーリング+config → 平文** です。CI 用の `ELABFTW_BASE_URL`+`ELABFTW_API_KEY`、通常は
キーリングの組、そしてキーリングのバックエンドが無いときだけ警告つきで config 平文にフォールバックします。プロファイルは
settings.json のように重なります（`config.toml` → `<project>/.elport.toml` → `<dir>/.elport.toml`、1 プロファイル 1 チーム）。
最初の `elport login` が既定になり、`elport profile use <name>` で切り替え（またはノートごとに `profile:` 指定）。
`.elportignore` は参照ファイルを `.gitignore` 形式で、上の層をまたいで加算的に除外します。

## さらに詳しく

- **なぜこの設計なのか** → [docs/DESIGN.md](docs/DESIGN.md)
- **eLabFTW の API が実際にどう振る舞うか** → [docs/ELABFTW-API.md](docs/ELABFTW-API.md)
- **動作の最終的な基準** → `tests/` のテストが権威です
- **AI エージェントから elport を動かす** → [skills/elport/SKILL.md](skills/elport/SKILL.md)

## 開発

構成は次のとおりです。`client.py` が API ラッパ、`transclude.py` が双方向の変換、`config.py`、`state.py` がベース、
`sync.py` が push/pull/status/diff、`cli.py` です。動作の最終的な基準はテストなので、変更するときはテストから直します。
ライブの API 挙動は <https://demo.elabftw.net> で確認できます。

## 関連

- [elab-doc-sync](https://github.com/Kosaku-Noba/elab-doc-sync)
- [elAPI](https://github.com/uhd-urz/elAPI)

## ライセンス

MIT
