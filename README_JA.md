# elport: eLabFTW のラボノートを手元で書いて同期する git ライクな CLI

[English](README.md) | [日本語](README_JA.md)

ローカル環境でお気に入りのエディタを使い、Markdown で実験ドキュメントを記述できます（eLabFTW 5.x では `<figure>`
などのインライン HTML も利用できます）。本文と、そこで参照されているすべての図やデータファイルをまとめて eLabFTW のエンティティへ
`push` / `pull` して同期します。**同期の基準は常にローカルです。**

## インストール

`elport` は macOS・Linux・Windows 向けの **単一静的バイナリ**（`CGO_ENABLED=0`、ランタイム依存なし）として配布しています。

[GitHub Releases](https://github.com/youthesame/elport/releases) からお使いの環境向けアーカイブをダウンロードし、`checksums.txt` で検証のうえ `elport`（Windows は `elport.exe`）を `PATH` の通った場所へ配置してください。

Go 1.26 以上があればソースから直接ビルドもできます:

```sh
go install github.com/youthesame/elport/cmd/elport@v0.2.0-alpha.1
```

### Python 版 `v0.1.x` からのアップグレード

既存の `~/.config/elport/config.toml` と `~/.config/elport/state/` は全 OS でそのまま引き継げます。

- **macOS**: Python 版が保存した Keychain エントリを初回読み込む際、`/usr/bin/security` による許可ダイアログが 1 回表示されます。「常に許可」を選ぶか、`elport login <profile>` を 1 回やり直してください。
- **Windows / KDE KWallet**: キーリング保存形式が異なるため、アップグレード後に 1 回 `elport login <profile>` を実行してください。
- **Python 版 `v0.1.x`**: [`python` ブランチ](https://github.com/youthesame/elport/tree/python) と PyPI に保全されています。

## コンセプト

ローカルの 1 ファイルが eLabFTW 上の 1 つのエンティティに対応します。YAML フロントマターを持つ Markdown ファイルとして記述します。

`push` を実行すると、elport は記法（Markdown 画像記法、HTML タグ、通常リンクなど）を問わず、
本文内で参照されている実在のローカルファイルを場所を問わず（`../data/raw.csv` や絶対パスも可。隠しファイルは除外）すべてアップロードし、各パスを正規の eLabFTW URL へと自動置換します。
本文は生の Markdown として送信されるため、数式（`$...$`）はそのままレンダリングされます。`<figure>` などのインライン HTML は
eLabFTW 5.x では描画されますが、eLabFTW 6.x では Markdown 本文中の生 HTML が表示時に除外されるため、6.x では標準の
`![alt](fig.png)` を使用してください。
別のエンティティにリンクしたい場合は、その eLabFTW URL かローカルのドキュメントのパスを書くだけです。コードブロック、インラインコード、HTML
コメントの内部は一切解析・置換されません。

## クイックスタート

```sh
elport login labA                       # base_url（config）と api_key（OS キーリング）を対話形式で保存
elport list                             # サーバー上にある自分のエンティティを確認（読み取り専用）
elport new "CRISPR titration"           # エンティティを作成し、id 入りの report.md 雛形を生成
# ...Web UI で作成済みのエンティティを編集するなら: elport clone <id>（読むだけなら: elport view <id>）
# ...report.md を編集し、同じ階層に fig1.png や data.csv を配置...
elport status                           # 同期予定の内容を確認（読み取り専用・サーバー送信なし）
elport push                             # 参照ファイルをアップロードし、本文を push
```

## ドキュメントの書き方

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
ディレクトリも渡せ、その下の elport ドキュメントを 1 行ずつ処理します。終了コードは成功時 `0`、失敗時 `1`、引数エラー時 `2` です。オプションの詳細は
`elport <command> --help` で確認できます。

### 同期

| コマンド | 概要 |
|---|---|
| `push` | 本文と参照ファイルをエンティティへアップロードします。 |
| `pull` | 本文とメタデータを取得し、参照ファイルをダウンロードします。 |
| `fetch` | 参照の有無にかかわらず、添付ファイルをすべてダウンロードします。読み取り専用。 |
| `status` | 同期予定の内容と動作モードを表示します。読み取り専用。 |
| `diff` | ローカルとリモートの差分。`--base` で直近 push 時との差分。 |
| `merge` | 競合後、`.base.md` と `.remote.md` を `<doc>` に 3 方向マージします。`git` が必要です。 |

`push` は `id` が未設定の場合にエンティティを新規作成し、`id` をフロントマターへ書き戻します。実行前にモード検証と
競合チェックを行います。`merge` はローカル完結で `git merge-file` を呼び出すため `git` のインストールが必要ですが、
フォルダを git リポジトリにする必要はありません。

### エンティティ操作

| コマンド | 概要 |
|---|---|
| `new "<title>"` | エンティティを新規作成し、フロントマターの雛形を生成します。 |
| `clone <id>` | 既存エンティティを編集して push するためのドキュメントを起こし、pull します。読むだけなら `view` を使います。 |
| `clone --all <dir>` | すべてのエンティティを `<dir>` にバックアップします。再実行で新しいものを追加します。HTML エンティティは `view -o` と同じ形で保存します。 |
| `list` | リモートのエンティティを `id  日付  タイトル` 形式で一覧表示します。読み取り専用。 |
| `view <id>` | リモートのエンティティを読みます。本文を読みやすい形にして標準出力へ表示します。読み取り専用です。HTML エンティティは書式を除き、添付はファイル名で示します。リンク・チェックリスト steps は標準エラーに出力します。 |
| `view <id> -o <dir>` | 本文とすべての添付ファイルを、新規または空の `<dir>` に書き出して読みます。作業コピーにはなりません。 |
| `activity` | 自分が作成したエンティティを日ごとのヒートマップで表示します（端末幅に収まる範囲で最大 1 年）。`--team` で著者ごとに表示します。 |
| `comments [<id>]` | リモートのコメントスレッドを表示します。`<id>` や URL を渡せばローカル文書は不要です。 |
| `comment "<text>"` | コメントを 1 件投稿します。編集・削除は Web UI で行ってください。 |

`new` と `clone` は `-o <doc>` を指定しない限り `report.md` に書き出します。`list`
はデフォルトで自分のエンティティのみ（`--scope self`）を 50 件ずつ返し、`-q <term>`、`--tag <tag>`、`--limit`、`--offset`、`--json`
で絞り込めます。`--all` を付けると全件をページ送りで取得します。`comments` はターミナルに出力するだけで、本文ファイルには書き込みません。

Web UI のリッチテキストエディタで書いたエンティティは本文が HTML で保存されます。elport はこれを読むだけです。`clone`、`pull`、
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

詳しい説明は CLI バイナリに埋め込まれているため、インストールした版と必ず一致します（英語）。

- `elport help documents`: フロントマター、参照、添付、ドキュメント間のリンク（[GitHub で読む](internal/help/documents.md)）
- `elport help workflow`: status、diff、push の流れ、競合、pull と fetch（[GitHub で読む](internal/help/workflow.md)）
- `elport help agents`: AI エージェントが実行前にユーザーへ確認すべきこと（[GitHub で読む](internal/help/agents.md)）

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
で切り替えるか、各ドキュメントのフロントマターで `profile:` を指定して個別に固定できます。

### .elportignore

`.elportignore` は `.gitignore` 形式で、ドキュメントのディレクトリ内にある参照ファイルをアップロード対象から除外します。
プロファイルと同じ設定階層に加えて、プロジェクトルートからドキュメントのあるディレクトリまでのすべての
`.elportignore` が加算的に適用されます。各パターンは、それが置かれたディレクトリを基準に解釈されます。

## さらに詳しく

- **eLabFTW API の実際の挙動と留意点** → [docs/ELABFTW-API.md](docs/ELABFTW-API.md)
- **動作仕様の正式な契約** → Go テスト（`internal/**/*_test.go`）、`testdata/golden/`、Quint トレース（`spec/quint/sync.qnt` → `testdata/quint/`）
- **AI エージェントから elport を操作する** → `elport help agents`。[skills/elport/SKILL.md](skills/elport/SKILL.md) の skill はそこを案内するだけです
- **旧 Python `v0.1.x` の設計ドキュメント・ADR 履歴** → [`python` ブランチ](https://github.com/youthesame/elport/tree/python) に保全されています

## 開発

| パッケージ | 役割 |
|---|---|
| `cmd/elport` | CLI エントリポイント・シグナル処理 |
| `internal/cli` | フラグ解析・ディスパッチ・`new`/`clone`・バッチ処理 |
| `internal/sync` | `push` / `pull` / `merge` / `fetch` / `status` / `diff` / `view` / `comments` |
| `internal/plan` | アップロード承認境界（`ApprovedUploadPath`） |
| `internal/transclude` | 手書き参照スキャナとパス ↔ URL 相互変換 |
| `internal/frontmatter` | YAML 1.1 互換フロントマターのパースと出力 |
| `internal/client` | HTTPS eLabFTW REST v2 クライアント（無通信タイムアウト・同一オリジン保護） |
| `internal/browse` | `list`・`activity` ヒートマップ |
| `internal/state` | 同期ベースとハッシュの管理 |
| `internal/config` | 階層設定・プロファイル・リダクト付き `APIKey` 解決 |
| `internal/fsx` | `*os.Root` 閉じ込め書き込みとアトミック置換 |
| `internal/help` | 埋め込みヘルプトピック（`elport help`） |
| `arch_test.go` | 構造ガードレールと `AGENTS.md` シンボル実在検証 |

**テストが動作仕様の唯一の権威であるため、仕様を変更する際は必ずテストから先に更新してください。** `go test -race ./...` と `go vet ./...` で検証できます。実際の API 挙動は公開デモ環境 <https://demo.elabftw.net> で確認できます。

## 謝辞

- [elab-doc-sync](https://github.com/Kosaku-Noba/elab-doc-sync)
- [elAPI](https://github.com/uhd-urz/elAPI)

## ライセンス

MIT
