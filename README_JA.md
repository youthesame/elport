# elport

[English](README.md) | [日本語](README_JA.md)

研究者と AI エージェントのための eLabFTW 用コマンドラインツールです。
ターミナルからラボのエンティティを読み取り・検索でき、手元で書いた Markdown ドキュメントを図やデータごと eLabFTW へ同期できます。手元のファイルが常に原本です。

- **読む・探す**: エンティティの検索や閲覧、リビジョン・コメント・添付ファイルの確認、手元への一括保存ができます。
- **書く・同期する**: ローカルの Markdown ドキュメント 1 件が eLabFTW のエンティティ 1 件に対応し、`push` は参照ファイルを自動アップロードして競合確認後に送信します。
- **安全に使える**: 削除機能を持たず、ローカルの読み取り専用モードを備え、テレメトリも送信しません。

## インストール

**macOS / Linux（Homebrew）:**

```sh
brew install youthesame/tap/elport
```

**macOS / Linux（シェルスクリプト）:**

```sh
curl -fsSL https://raw.githubusercontent.com/youthesame/elport/main/install.sh | sh
```

**Windows (PowerShell):**

```powershell
irm -UseBasicParsing https://raw.githubusercontent.com/youthesame/elport/main/install.ps1 | iex
```

<details><summary>その他の方法（Nix / Go）</summary>

**Nix**（`nix-command` と `flakes` が有効な環境）:

```sh
nix profile install github:youthesame/elport
```

インストールせずに試す場合:

```sh
nix run github:youthesame/elport -- --help
```

**Go 1.26+:**

```sh
go install github.com/youthesame/elport/cmd/elport@latest
```

</details>

<details><summary>Python 版 v0.1.x からのアップグレード</summary>

既存の `~/.config/elport/config.toml` と `~/.config/elport/state/` は全 OS でそのまま引き継げます。削除しないでください。

1. 先に Python 版をアンインストールしてください（`uv tool uninstall elport` / `conda global uninstall elport` / `pipx uninstall elport` / `pip uninstall elport`）。残っていると `PATH` 上で旧版が優先されることがあります。
2. 上の手順で Go 版をインストールし、`command -v elport`（PowerShell は `Get-Command elport`）と `elport --version` を確認してください。

- **macOS**: Python 版が保存した Keychain エントリを初回読み込む際、`/usr/bin/security` の許可ダイアログが 1 回表示されます。「常に許可」を選ぶか、`elport login <profile>` を 1 回やり直してください。
- **Windows / Linux (KDE)**: ネイティブキーリングの保存形式が異なるため、アップグレード後に 1 回 `elport login <profile>` を実行してください。
- **Python 版 `v0.1.x`**: [`python` ブランチ](https://github.com/youthesame/elport/tree/python) と PyPI に保全されています。

</details>

## クイックスタート

### 読む・探す

```sh
elport login labA                       # base_url を config、api_key を OS キーリングに保存
elport list --scope team -q "CRISPR"    # チームのエンティティを検索
elport view 42                          # エンティティ本文を表示
elport revisions 42                     # 保存済みリビジョン一覧を表示
```

### 書く・同期する

```sh
elport new "CRISPR titration"           # エンティティを作成し、report.md を生成
# ...report.md を編集し、近くに fig1.png や data/raw.csv を配置...
elport status                           # 同期予定を確認
elport push                             # 参照ファイルをアップロードし、本文を push
```

ドキュメントは YAML フロントマター付きの Markdown です。

```markdown
---
id: 42
title: CRISPR titration
tags: [CRISPR]
---

# Result

![titration curve](fig1.png)

Raw data: [raw.csv](data/raw.csv)
```

`push` は参照先のローカルファイルを自動アップロードし、手元のファイルを書き換えずにサーバー側のパスを eLabFTW URL へ置換して送信します。参照ルールやエンティティ間リンクの詳細は `elport help documents` を参照してください。

<details><summary>フロントマターのキー一覧</summary>

| キー | 値・型 | 既定 / 挙動 |
|---|---|---|
| `id` | 正の整数 | `new` または初回 `push` で自動付与（手書き不要） |
| `entity` | `experiments` \| `items` | 既定 `experiments` |
| `format` | `markdown` \| `html` | 既定 `markdown`（`html` は一方向公開、下記参照） |
| `title` | 文字列 | 初回作成時はファイル名が既定 |
| `tags` | リスト（単一値も可） | 追加のみ。`pull` ではローカルとリモートの和集合（削除は Web UI） |
| `category` | ID または既存カテゴリ名 | 新規作成はしない |
| `status` | ID または既存ステータス名 | 新規作成はしない |
| `profile` | プロファイル名 | このドキュメントの接続先サーバーを固定 |
| `read` | `owner` \| `owner+admin` \| `team` \| `account` \| `public` | 基本公開範囲のみ変更（Web UI の個別許可は維持）。`team` 超の拡大時は確認プロンプト |
| `write` | `read` と同じ 5 段階 | `read` と同じルール |
| `attachments` | ローカルパスのリスト | 本文から参照せずにアップロードしたい添付ファイル |

- **同期ルール**: `push` は前回同期から変更したキーだけを送信します（未編集・削除したキーはリモート値を維持）。未知のキーはエラーになります。
- **`format: markdown`（既定）**: Markdown のまま送信し、`push` / `pull` / `merge` で双方向同期します。eLabFTW 5.x では `<figure>` 等のインライン HTML が表示されますが、eLabFTW 6.x では Markdown 本文中の生 HTML が除去されます。
- **`format: html`（一方向公開）**: `elport new --format html "<title>"` で作成（既存の同期済みドキュメントに追記して移行も可能）。`push` 時に Markdown を HTML へ変換して送信し、`<figure>`・`<img width>`・`<sub>`/`<sup>`・`<s>`・表内 `<br>`・`$...$` 数式を保持します。`pull` はフロントマターのみ更新し、Web UI 側が編集された場合は `elport diff` 確認後に `elport push --confirm-remote <hash>` で上書きします（Markdown へ戻すには明示的に `format: markdown` を指定）。

</details>

## AI エージェントとの連携

elport は単一静的バイナリとして動作し、MCP サーバーや追加ランタイムなしで AI エージェントから直接利用できます。

- **内蔵ガイド**: `elport help agents` を実行するか、[skills/elport/SKILL.md](skills/elport/SKILL.md) を導入すると、インストール済みの版に一致したガイドをエージェントが参照できます。
- **読み取り専用モード**: `ELPORT_READ_ONLY=1` または設定の `read_only = true` で、`new`・`comment`・ドライランなしの `push` をローカル側で拒否できます。
- **安全な出力**: パイプ時は `--json`・`revisions --rev`・`view --attachment` を生データのまま出力し、端末表示では制御文字をエスケープします。

## コマンド一覧

ドキュメント対象コマンドの既定は `report.md` で、同期コマンドはディレクトリの一括処理にも対応します。終了コードは成功 `0`、失敗 `1`、引数エラー `2` です。詳細は `elport <command> --help` で確認できます。

### 読む・探す

| コマンド | 概要 |
|---|---|
| `list` | エンティティを一覧・検索します（`-q`、`--tag`、`--scope`、`--order`、`--all`、`--json`）。 |
| `view <id>` | 本文を表示します。`-o <dir>` で添付込み保存、`--json` で生 JSON、`--attachment <name>` で添付 1 件を出力。 |
| `revisions [<doc-or-id>]` | 本文のリビジョン一覧を表示します。`--rev <id>` で 1 件の本文をそのまま出力。 |
| `comments [<id>]` | リモートのコメントを端末に表示します。 |
| `activity` | 作成数の日次ヒートマップを表示します（`--team` で著者別）。 |
| `fetch` | 本文を変更せず、添付ファイルをすべてダウンロードします。 |

### 書く・同期する

| コマンド | 概要 |
|---|---|
| `new "<title>"` | エンティティを新規作成し、ローカルドキュメントを生成します。 |
| `clone <id>` | 既存エンティティの作業コピーを作成します（`--all <dir>` で一括ミラー）。 |
| `status` | 同期予定と動作モードを表示します。 |
| `diff` | ローカルとリモートの差分を表示します（`--base` で直近同期との差分）。 |
| `push` | 競合確認と参照ファイルアップロード後に本文を送信します。 |
| `pull` | リモートの変更と参照ファイルをローカルへ取得します。 |
| `merge` | 競合後に `git` で 3 方向マージします。 |
| `comment "<text>"` | エンティティへコメントを投稿します。 |

### セットアップ

| コマンド | 概要 |
|---|---|
| `login [<profile>]` | `base_url` を `config.toml`、`api_key` を OS キーリングに保存します。 |
| `logout [<profile>]` | 保存済みの `api_key` を削除し、`base_url` は残します。 |
| `profile [use <name>]` | プロファイル一覧を表示、または既定を切り替えます。 |
| `whoami` | 認証状態、サーバーバージョン、読み取り専用状態を確認します。 |

**共通オプション**: `-n`/`--dry-run` は送信せずリハーサル、`-f`/`--force` はリモート変更を上書き、`-y`/`--yes` は確認プロンプトをスキップ、`--profile <name>` と `--entity {experiments,items}` で対象を指定します。

## セキュリティ・認証・設定

**プロジェクト内に認証情報を保存しません。テレメトリもありません。** elport CLI が eLabFTW にリクエストを送る先は、設定した `base_url` の HTTPS オリジンに限られます。別オリジンへのリダイレクトは拒否します。CLI はテレメトリの送信、利用状況の収集、アップデートの確認を行いません。

- **認証情報**: API キーは OS キーリング、非機密設定は `~/.config/elport/config.toml`（パーミッション `600`）に保存されます。優先順は `ELABFTW_BASE_URL` + `ELABFTW_API_KEY` 環境変数、OS キーリング、キーリング非搭載環境のみ平文フォールバックです。
- **プロファイル**: `~/.config/elport/config.toml` → `<project>/.elport.toml` → `<dir>/.elport.toml` の順に階層適用され、フロントマターの `profile:` でファイル単位に固定できます。
- **読み取り専用モード**: `ELPORT_READ_ONLY=1` またはいずれかの設定階層のトップレベル `read_only = true` で書き込みを拒否し、下位階層では解除できません。サーバー側での強制には読み取り専用の eLabFTW API キーを使ってください。
- **`.elportignore`**: `.gitignore` 形式でドキュメントディレクトリ内のファイルをアップロード対象から除外します。

## ドキュメントと開発

詳しい使い方はバイナリに埋め込まれており、インストール済みのバージョンと常に一致します。

- `elport help documents`: フロントマター、参照、添付、`format: html`、エンティティ間リンク（[原文](internal/help/documents.md)）
- `elport help workflow`: 同期ループ、競合、`pull` と `fetch`（[原文](internal/help/workflow.md)）
- `elport help agents`: エージェント向けワークフロー、確認ルール、信頼できないリモート内容の扱い（[原文](internal/help/agents.md)）

リポジトリ内のドキュメント:

- [docs/ELABFTW-API.md](docs/ELABFTW-API.md): 実測した eLabFTW REST API の挙動
- [AGENTS.md](AGENTS.md): 不変条件、アーキテクチャ境界、検証コマンド（`go vet ./... && go test -race ./...`。ライブ確認は <https://demo.elabftw.net> のみ）
- [`python` ブランチ](https://github.com/youthesame/elport/tree/python): 旧 Python `v0.1.x` と過去の ADR 履歴

## 謝辞

- [elab-doc-sync](https://github.com/Kosaku-Noba/elab-doc-sync)
- [elAPI](https://github.com/uhd-urz/elAPI)

## ライセンス

MIT
