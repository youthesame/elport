# elab — eLabFTW のラボノートを手元で書いて同期する git ライクな CLI

[English](README.md) | [日本語](README_JA.md)

ラボノートを手元のエディタで Markdown として書き、`<figure>` などの HTML 断片も混ぜられます。図やデータファイルごと
eLabFTW へ `push` / `pull` します。同期の基準になるのはいつも手元のファイルです。本文から参照されている添付ファイルも
まとめて運びます。

- なぜこの設計なのか → [docs/DESIGN.md](docs/DESIGN.md)
- eLabFTW の API が実際にどう振る舞うか → [docs/ELABFTW-API.md](docs/ELABFTW-API.md)
- 動作の最終的な基準は `tests/` のテストです
- AI エージェントから elab を動かす → [skills/elab/SKILL.md](skills/elab/SKILL.md)

## インストール

Python 3.10 以上が必要です。`uv` ツールとして入れると `elab` コマンドが使えます。

```sh
uv tool install git+https://github.com/youthesame/elab
```

## 中心となる考え方

手元の 1 ファイルが eLabFTW 上の 1 件の記録に対応します。記録には実験とデータベース項目の 2 種類があり、どちらにも
push できます。ファイルは YAML フロントマターを付けた Markdown で、`<figure>` などの HTML 断片も書けます。

`push` すると、elab は本文が参照している実在のローカルファイルを記法によらず集めてアップロードし、そのパスを本物の
eLabFTW の URL に置き換えます。`width` や alt、`<figcaption>` は残り、本文は生の Markdown として送られるので
`<figure>` や `$...$` はそのまま表示されます。

別の記録は、その eLabFTW の URL をそのまま書けばアップロードせずリンクできます。コードフェンス・インラインコード・HTML
コメントの中は解析しません。

## クイックスタート

```sh
elab login labA                       # base_url を config に、api_key を OS キーリングに保存。対話式
elab new "260809 CRISPR titration"    # 記録を作成し、elab_id 付きの report.md を雛形生成
# ...report.md を編集し、fig1.png や data.csv を隣に置く...
elab status                           # 何が同期されるか確認。送信はしない
elab push                             # 参照ファイルをアップロードして本文を push
```

## コマンド

デフォルトのドキュメントは `report.md` です。名前は自由に付けられます。成功で終了コード `0`、失敗で `1` を返します。

| コマンド | 概要 |
|---|---|
| `elab push [<doc>]` | `<doc>` を 1 件の記録に push します。記録が無ければ作成し `elab_id` を書き戻します。先にモードとコンフリクトを確認します。 |
| `elab pull [<doc>]` | 本文を取得し、URL をローカルパスへ戻し、参照ファイルをダウンロードします。 |
| `elab status [<doc>]` | 副作用なし。ローカルの変更、リモートの編集、アップロードされるファイル、モードを表示します。 |
| `elab diff [<doc>]` | ソース形式の差分。デフォルトはローカルとリモートの比較で、`--base` を付けるとローカルと最後の push の比較になります。送信しません。 |
| `elab merge [<doc>]` | コンフリクト後、`.base.md`/`.remote.md` サイドカーを `git merge-file` で `<doc>` に3-wayマージします。ローカルのみ・git は任意。 |
| `elab comments [<doc>]` | リモートのコメントスレッドを表示します（端末のみ。本文には書き込みません）。 |
| `elab comment [<doc>] "<text>"` | コメントを1件投稿します（編集・削除はしません。Web UI で行ってください）。 |
| `elab new "<title>" [--entity experiments\|items] [--profile <name>] [-o <doc>]` | 記録を作成し、フロントマターの雛形を生成します。 |
| `elab whoami [--profile <name>]` | 認証を確認し、ユーザーと現在のチームを表示します。 |
| `elab login [<profile>]` | `base_url` を `config.toml` に、`api_key` を OS キーリングに保存します。入力はプロンプト式で、キーは画面に出ません。 |
| `elab logout [<profile>]` | プロファイルの保存済み `api_key` をキーリングと config の平文から削除し、`base_url` は残します。 |

オプション:

- `-n` / `--dry-run` … push のリハーサル。送信しません。
- `--profile <name>` … 使うプロファイル。解決順はフロントマター、CLI、デフォルトの順です。
- `-f` / `--force` … 変更済みのリモートに上書きで push します。Web 側の変更は失われます。
- `-y` / `--yes` … push で `read`/`write` を `account`/`public` へ広げるときの確認を省略します。
- `--entity experiments|items` … フロントマターの指定が優先されます。

> pull でファイルを書き戻すときはベース名だけを使います。`assets/fig.png` のようなサブディレクトリ付きのパスは
> `fig.png` に平坦化されます。

## ドキュメント形式 — フロントマター

先頭に YAML ブロックを置きます。無ければ push 時に生成・補完されます。

```markdown
---
elab_id: 42            # 記録の ID。作成時に自動で埋まる
entity: experiments    # experiments か items。デフォルトは experiments
title: "260806 experiment title"
tags: [CRISPR, PCR]    # 任意。追加のみ
category: Molecular Biology   # 任意。ID または既存のカテゴリ名
profile: labA          # 任意。送信先プロファイル
read: team             # 任意。owner | owner+admin | team | account | public
write: owner           # 任意。同じ段階
---

# 本文。Markdown で書き、HTML 断片も混ぜてよい ...
```

- 保持するのは `elab_id` と人が読むメタデータ、任意の `profile` だけです。ベースやハッシュは state に持ち、ここには
  置きません。
- `title` と `category` は反映されます。`tags` は追加のみで、削除は Web UI で行います。フロントマターは本文を送る前に
  取り除かれます。
- `read`/`write` は eLabFTW の**ベース公開範囲**を設定します。書いたときだけ反映し、無ければ権限には一切触れません。
  送るのはベース段階のみなので、Web UI で設定した個別共有は保持されます。`account`/`public`（チーム外）へ広げるときは
  確認します（`-y` で省略。非対話では `-y` が必要）。
- フロントマターと CLI が profile や entity、elab_id で食い違ったら、elab は推測せずに停止します。

## コンフリクト

本文は Web UI でも編集できるため、`push` はまず現在のリモートを保存済みのベースと比べます。

- 変更なし … そのまま続行します。
- 変更あり … 中止します。メッセージは `remote changed; use pull or --force` です。elab は `<name>.base.md`（祖先）と
  `<name>.remote.md` を出力するので、`elab merge` で `<doc>` に3-wayマージし、`<<<<<<<`/`>>>>>>>` マーカーを解消してから
  push します。手動でマージしたい場合は、サイドカーを本文に取り込んだあと push の前に `elab merge --resolved` を実行して
  ください（このステップが取り込んだリモートを記録するので、push が同じコンフリクトを再度報告しません）。マーカーが残った
  本文は push が拒否します。
- このマシンにベースが無い … 中止します。先に `elab pull` するか、`--force` で上書きします。

`--force` は Web 側の変更を捨てます。意図して使ってください。eLabFTW は Web UI から復元できるサーバー側の履歴を安全網
として持っています。ただし毎回の保存ごとに残るわけではありません。詳しくは [docs/ELABFTW-API.md](docs/ELABFTW-API.md)
を見てください。

> elab 自身は手元の履歴を持ちません。ベースは `~/.config` にあり、tree には入りません。編集ごとの履歴が欲しければ
> ノートフォルダで `git init` してください。ファイルはただの Markdown です。

## 設定と認証

資格情報はプロジェクトの中に置きません。API キーは OS キーリングに保存し、`elab login` で設定します。キーリングは
macOS の Keychain、Windows の Credential Manager、Linux の Secret Service です。base_url など機密でない値は
`~/.config/elab/config.toml` に保存され、ファイルのモードは `600` です。キーは決して表示されません。

解決順は次のとおりです。

1. 環境変数 `ELABFTW_BASE_URL` と `ELABFTW_API_KEY`。両方セットしたときに有効で、主に CI 用です。
2. キーリングと `config.toml`。通常はこれです。
3. キーリングのバックエンドが無いときは環境変数の利用を促します。config の平文キーにフォールバックするときは、はっきり
   警告します。

プロファイルは settings.json のように層で重なって上書きされます。順に `~/.config/elab/config.toml`、プロジェクト直下の
`.elab.toml`、各ディレクトリの `.elab.toml` です。どの層も base_url などを持てますが、キーは常にキーリングに入り、
1 つのプロファイルは 1 つのチームに対応します。

```toml
# ~/.config/elab/config.toml
default_profile = "labA"

[profiles.labA]
base_url   = "https://elab-a.example.org"
verify_ssl = true
```

`.elabignore` は参照ファイルに `.gitignore` と同じ形式の除外を与えます。上の層をまたいで加算的に働きます。

## 開発

構成は次のとおりです。`client.py` が API ラッパ、`transclude.py` が双方向の変換、`config.py`、`state.py` がベース、
`sync.py` が push/pull/status/diff、`cli.py` です。動作の最終的な基準はテストなので、変更するときはテストから直します。
ライブの API 挙動は <https://demo.elabftw.net> で確認できます。バージョンに依存する挙動は実際の対象インスタンスでも
確認してください。

## 関連

- [elab-doc-sync](https://github.com/Kosaku-Noba/elab-doc-sync)

## ライセンス

MIT
