# elab — eLabFTW ローカル執筆・同期 CLI 仕様書

- **ステータス**: ドラフト（設計レビュー済み）
- **作成日**: 2026-08-06 / **最終更新**: 2026-08-07
- **対象読者**: この会話の文脈を持たない第三者の実装／レビューエージェント
- **ツール名 / コマンド**: `elab`。eLabFTW 用の**ユーザー向け同期 CLI**（git 的に push/pull する）。
  `gh`（ツール）と GitHub（サービス）の関係と同様、ツール名 `elab` と接続先サービス `eLabFTW` は別。
  管理系の `elabctl` や API ライブラリ `elabapi` とも役割が分かれる。既存の `esync`（elab-doc-sync）とも別物。
  Markdown 専用ではなく（md/HTML/任意添付を扱う）、旧称 `elabmd` から改称。
- **実装形態**: Python 3.10+ / `uv tool` でインストールする CLI

---

## 0. このドキュメントの目的

`elab` は「ローカルで書いた実験ノート（Markdown、HTML 混在可）を eLabFTW に push し、
必要なら pull で取り戻すツール」である。本書は **何を・なぜ・どう作るか** を、前提知識ゼロの
実装者が再議論なしに着手・レビューできるように記述する。特に **設計判断の経緯**（§1）を残す。

本仕様は 2026-08-07 の設計セッションと、その後の外部レビューを反映した結果である。中核方針は
確定しているが、対象インスタンス依存の互換性項目（§10.2）は実装前テストで固定する。**§3（中核
エンジン）、§6（認証・設定）、§8（API 事実）、§9（同期セマンティクス）は設計の核なので逸脱しないこと。**

---

## 1. 背景と経緯

### 1.1 出発点

ユーザーは研究者で、eLabFTW をラボノートとして使う。ローカルの好きなエディタで
Markdown/HTML を書き、eLabFTW のエントリに同期したい。まず既存 OSS `elab-doc-sync`
（CLI 名 `esync`, <https://github.com/Kosaku-Noba/elab-doc-sync>）を試用したが、精読の結果、
ユーザーの執筆スタイルと根本的に噛み合わない仕様が判明し、薄い自作ツールに置き換える判断に至った。

### 1.2 `elab-doc-sync` の実挙動（精読で確定した事実）

参照ソース: `~/.local/share/uv/tools/elab-doc-sync/lib/python3.14/site-packages/elab_doc_sync/`。

1. **画像アップロードは Markdown 画像記法 `![alt](path)` のみが対象**（`IMAGE_RE`）。
2. **HTML の `<img src>` や通常リンク `[text](path)` は対象外**（アップロードも URL 書換もされない）。
3. 非画像添付は `attachments_dir` フォルダ走査（フラット・非再帰、画像拡張子は除外）で処理。
4. `attachments_dir` はターゲット内の全エンティティに共有（`each` でも実験ごとに出し分け不可）。
5. `each` の「ファイル→エンティティ」対応は basename キー。
6. **`content_type` を一切送っていない**（body 送信だけ。md/html の解釈はサーバー側任せ）。
7. **pull は HTML→Markdown を `markdownify` で逆変換**して `.md` に保存する（往復劣化する）。

### 1.3 ユーザーの執筆スタイルと要件

- 図は HTML で書く（`<figure><img src="..." width="80%"><figcaption>…</figcaption></figure>`）。
  既定の Markdown 画像表示が嫌いで、常にこの形にしている。
- LaTeX 数式（`$...$` / `$$...$$`）を使う。
- 明示的な制御を好む。自動の競合検出や双方向同期の“気を利かせる”挙動は最小限でよい。
- push 中心（ローカルが source of truth）。ただし **eLabFTW の Web UI で本文を編集することがある**。
- 機密（API キー）を**プロジェクトに置きたくない**（GitHub 誤コミット事故を避けたい）。

### 1.4 なぜ自作するか（不一致の核心）

`elab-doc-sync` は「`![]()` という 1 記法だけを特別扱いして自動アップロードするおまけ」設計で、
ユーザーの HTML `<figure>` スタイルは自動化の対象外になる。ユーザーが本当に欲しい動作は逆の一般化：

> **記法に依存せず、本文が参照している実在ローカルパスを「すべて」アップロードし、そのパスを
> eLabFTW の実 URL に置換する。**

この 1 エンジンで、`<img src="fig.png">` も `[data](x.csv)` も `![a](f.png)` も同じ規則で
「アップロード＋実 URL 化」され、非対称・切れリンク・figure 非対応がまとめて解消する。

### 1.5 方針：`elab` を新規に作り、`elab-doc-sync` は「参照元」とする

`elab` は新規のクリーンな設計で作る（フォーク＝既存コード継承ではない）。ただし
`elab-doc-sync` はよく出来た**参照実装**であり、API クライアント、変更検知、逆 transclusion
（`_download_images`）など、無い機能が欲しくなったら**そこを参照して必要分だけ取り込む**。

---

## 2. スコープ

### 2.1 設計原則

- **単一エンジン**: 「参照ローカルパス → アップロード＋実 URL 置換」（記法・種別非依存）。§3。
- **markdown ネイティブ**: 本文は生 Markdown を送る（§9.1）。変換で崩さない。
- **明示・単純**: 隠れた自動処理を増やさない。ローカルが正、push は本文全文上書き（競合検出付き）。
- **機密隔離**: 認証情報はホーム配下＋OS 保管庫にのみ置く。プロジェクトに機密を置かない。§6。
- **薄く保つ**: 欲しくなった機能は `elab-doc-sync` を参照して必要分だけ足す（§1.5）。

### 2.2 スコープ内（MVP）

- 単一ドキュメント（`report.md` 等）を 1 エンティティとして **push**（§9.1）。
- 本文が参照する実在ローカルファイルを**記法・種別問わず**アップロードし、参照を実 URL に置換（§3）。
- 生 Markdown 送信（`content_type=2`）。`<figure>` も数式も eLabFTW 側でそのまま描画（§9.1・§8）。
- **pull**（逆 transclusion 付き、§9.2）。
- **競合検出＋ git 委譲マージ**（§9.3）。
- **状態確認 `status` と差分 `diff`**（副作用なしの読み取り系。§9.5）。git 的中核 UX。
- YAML フロントマターによる `elab_id` / `title` / `tags` / `category`（+任意 `profile`）管理（§5）。
- 複数プロファイル（複数チーム／インスタンス）と階層設定（§6）。
- 認証・接続確認（`whoami`）、`.elabignore` による除外と push 時アップロード一覧表示（§9.1）。

### 2.3 スコープ外（当面やらない）

- ツール内マージエンジンの実装（マージは **git に委譲**、§9.3）。
- 双方向の自動同期・自動衝突解決。
- `attachments_dir` のようなフォルダ規約（§3 の参照ベース方式で不要）。
- GUI・TUI。

---

## 3. 中核エンジン仕様（Path Transclusion）

push 時、本文（送信用のコピー）に対して以下を行う。**ローカル原本は書き換えない**。

### 3.1 参照の抽出

本文テキストから「ファイルパスを含む参照」を抽出する（**Markdown / HTML を問わない**）。最低限：

- Markdown: `![alt](PATH)` と `[text](PATH)`（`]( ... )` の中身）
- HTML 属性: `src="PATH"` / `src='PATH'`、`href="PATH"` / `href='PATH'`

**解析対象外の範囲（重要）**: 次の内部に現れるパスは参照として扱わない（コード例に書いたリンクを
実ファイルとして誤アップロードしないため）。

- Markdown のフェンス付きコードブロック（```` ``` ```` / `~~~`）とインラインコード（`` `...` ``）
- HTML コメント `<!-- ... -->`、`<pre>` / `<code>` の内部

> 本仕様書 §5 の例自体が ```` ```markdown ```` フェンス内に `[data.csv](data.csv)` を含むように、
> ドキュメントにはコード例としてのリンクが普通に現れる。除外は誤爆防止の必須要件。

### 3.2 対象判定（アップロードするパスか）

抽出した `PATH` のうち、次を**すべて満たすものだけ**を対象にする：

- スキームを持たない（`http://` `https://` `data:` `mailto:` `#anchor` は除外）。
- ドキュメントのあるディレクトリ基準で解決したとき、**実在するファイル**である
  （`document_dir / PATH`。存在しなければ対象外＝警告して素通り）。
- **ドキュメントディレクトリ配下に収まる**（安全境界。誤送信防止）。
  - 絶対パス・`..` による親への脱出・UNC パスは対象外にして警告する。
  - 判定は `(document_dir / PATH).resolve()` の結果が `document_dir.resolve()` 配下かで行う。
    `resolve()` はシンボリックリンクも解決するので、リンク経由の配下外脱出もこの 1 判定で弾ける。
  - 例: `[debug](../../.ssh/id_rsa)` は配下外なのでアップロードされない。
- `.elabignore`（§9.1）のパターンに一致しない。

> **拡張子除外はしない。** 図・CSV・PDF も `.md` / `.html` も、参照された実在ローカルファイルは
> 種別問わず全て対象（`.md` を意図的に添付することもあるため）。
>
> **「添付」と「他エンティティへのリンク」の切り分け**（重要）:
> - ファイルとして**添付**したい → **ローカルパス**で参照 → アップロード＋実 URL 置換。
> - 別 eLabFTW エンティティへ**リンク**だけしたい（アップロード不要）→ 相手の **eLab URL（http）**
>   で参照 → 除外されるのでアップロードされず、クリック可能なリンクになる。

### 3.3 アップロードと置換

対象パスごとに：

1. 対象エンティティにアップロード（§8.4）。重複回避は §9.4。
2. 実 URL `{base_url}/app/download.php?f={long_name}&name={real_name}&storage={storage}` を得る。
   - **クエリ値は percent-encode する**（`urllib.parse.urlencode`）。`real_name` にスペース・日本語・
     `&`・`#` を含むと文字列補間では壊れる。逆変換（§3.4）もクエリ順序を仮定せず URL パーサで
     `f` / `name` / `storage` を取得する。
3. 本文中のその `PATH` を実 URL に置換（URL 部分のみ。`width` や alt、`<figcaption>` 等は保持）。
   - **置換は §3.1 で抽出したスパン（オフセット範囲）に対してのみ行う**。文字列の全域置換はしない
     （コードフェンス内の同一文字列や部分一致（`fig.png` ⊂ `myfig.png`）を巻き込むため。
     §3.1 の除外規則は抽出だけでなく置換にも及ぶ）。

同じファイルが複数回参照されても 1 回だけアップロードし、全出現箇所を同じ URL に置換する。

### 3.4 逆 transclusion（pull / マージ用）

pull やマージでは逆方向が要る。**このエンティティのアップロード URL（`app/download.php?...&name=X`）
を、ローカルの basename パス（`X`）に書き戻す**。画像等の実体はローカルへダウンロードして配置する。
**ダウンロードは `app/download.php` ではなく API の `GET /api/v2/{entity}/{id}/uploads/{upload_id}?format=binary`
を使う**（download.php は API キーで認証されず、API 経由なら実バイナリが取れる。実測 §8.4）。
URL からどの upload かは、`GET .../uploads` 一覧の `long_name`/`real_name` と body 中の URL クエリ
（`f`/`name`、URL パーサで取得）を突き合わせて特定する。**body 中の URL は HTML 属性内で `&` が
`&amp;` にエスケープされている（サーバー正規化、実測 §8.2）ので、URL 解析の前に HTML unescape する**。これにより remote 本文をソース形
（ローカルパス）に戻せる。§9.2 / §9.3 で使用。

> **可逆性の制約（正直な明記）**: 書き戻し先は basename のみで、元の相対パスのディレクトリ情報は
> 復元しない。§7 の推奨レイアウト（図もデータも `report.md` と同階層のフラット構成）では実運用上
> 問題にならないが、**サブディレクトリ（例 `assets/fig.png`）を使った場合、pull では `fig.png` に
> 平坦化される**。これは「完全な可逆変換」ではない。元パスを保持する対応表を state に持つ設計は
> 採らない（薄く保つ方針、§1.5）。

### 3.5 既知のエッジケース

- **basename 衝突**: eLab 上のファイル名は basename。別ディレクトリの同名ファイルは衝突しうる。
  扱いは **§9.4-4 に従う（同名で内容が異なればエラー中断）**。
- **解決できないパス**: 実在しなければ置換せず `stderr` に警告して素通り。
- **相対パスの基準**: 常に「ドキュメントのあるディレクトリ」（カレントではない）。

---

## 4. コマンド仕様

CLI 名 `elab`。ファイル名は固定しない（既定 `report.md`、任意名可）。成否は**終了コード**
（成功 `0` / 失敗 `1`）。付加コマンドが欲しくなれば `elab-doc-sync` を参照して移植する。

| コマンド | 概要 |
|---|---|
| `elab push [<doc>]` | `<doc>`（既定 `report.md`）を 1 エンティティに push（§9.1）。`elab_id` 未設定なら新規作成し frontmatter に書き戻す。push 前にモード確認（§9.1）と競合検出（§9.3）。 |
| `elab pull [<doc>]` | エンティティ本文を取得し、逆 transclusion してソース形で反映（§9.2）。 |
| `elab status [<doc>]` | 副作用なしで状態を表示：ローカルが base から変更されているか／リモートが Web 編集されたか／今回アップロードするファイル／モード（§9.5）。 |
| `elab diff [<doc>]` | ソース形で差分表示：ローカル ↔ リモート（既定）、`--base` でローカル ↔ base。逆 transclusion 済みで比較（§9.5）。送信はしない。 |
| `elab new "<title>" [--entity experiments\|items] [--profile <名前>] [-o <doc>]` | エンティティを新規作成し、`elab_id`/`title` を frontmatter に持つ雛形を生成。 |
| `elab whoami [--profile <名前>]` | 認証確認（`GET /api/v2/users/me`）。ユーザー／アクティブチーム表示。 |
| `elab login [<profile名>]` | プロファイル単位で認証登録：base_url を `config.toml`、api_key を keyring（§6）。 |

共通オプション：

- `-n, --dry-run`: 送信せず「何をアップロードし、どのパスをどの URL に置換するか」を提示（§9.1）。
- `--profile <名前>`: 使用プロファイル（＝チーム/インスタンス）。frontmatter → CLI → 既定の順（§6.5）。
- `-f, --force`: 競合検出で「リモート変更あり」でも強制上書き（Web 側変更は失われる。§9.3）。
- `--entity {experiments,items}`: 対象エンティティ種別。既定 `experiments`。frontmatter が優先。

---

## 5. ドキュメント形式とフロントマター

ドキュメント先頭に YAML フロントマターを置く（無ければ push 時に生成・補完）。

```markdown
---
elab_id: 42            # eLabFTW のエンティティ ID（新規作成時に自動記入）
entity: experiments    # experiments | items（既定 experiments）
title: "260806 実験タイトル"
tags: [CRISPR, PCR]    # 任意
category: 分子生物      # 任意（ID または名前。名前指定は既存カテゴリに解決、無ければ実装時方針）
profile: labA          # 任意。送信先プロファイル（§6.5）
---

# 本文（Markdown / HTML 混在可）

<figure>
<img src="fig1.png" width="80%">
<figcaption>図1. 結果</figcaption>
</figure>

測定データは [data.csv](data.csv) を参照。式は $\eta = 1 - e^{-kt}$。
```

push すると `fig1.png` と `data.csv` がアップロードされ、`src="fig1.png"` と `](data.csv)` が
実 URL に置換され、**生 Markdown として送信**される（§9.1）。`<figure>` も `$...$` も eLabFTW が描画。

- フロントマターに持つのは **`elab_id` と人間向けメタ（title/tags/category）＋任意 profile のみ**。
  base（push/pull 後にサーバーから読み戻した本文）や hash は frontmatter に**入れない**（§9.3・§6.6）。
- `title`/`category` は frontmatter を正として反映。**`tags` は追加のみ**（§8.5 のタグ API は追加のみ。
  frontmatter からタグを消しても eLab 側では削除しない。削除は Web UI で行う）。
- **フロントマターは本文送信前に除去する**（本文には含めない）。
- **YAML の読み書き**: `yaml.safe_load` を使う。未知のキーは保持する。`elab_id` の書き戻しは
  一時ファイルに書いてから atomic replace する（書き込み中断で原本を壊さない）。書式（コメント・
  引用符・キー順）は**正規化されうる**（書式保持は保証しない）。
- この「markdown 内 YAML フロントマター」方式は `elab` の第一級機能（`elab-doc-sync` は非採用）。

---

## 6. 認証・設定（機密隔離・クロスプラットフォーム・階層）

**原則: 認証情報はプロジェクトに一切置かない。機密は OS 保管庫、非機密はホーム配下。**

### 6.1 機密（API キー） — `keyring` で OS 保管庫

| OS | keyring バックエンド |
|---|---|
| macOS | Keychain |
| Windows | Credential Manager |
| Linux（デスクトップ） | Secret Service（GNOME Keyring / KWallet） |
| Linux（ヘッドレス/CI） | §6.4 のフォールバック |

- 保存: `keyring.set_password("elab", "<profile名>", api_key)`。
- 取得: `keyring.get_password("elab", "<profile名>")`。

### 6.2 非機密（base_url 等） — ホーム配下 TOML

- `~/.config/elab/config.toml`（パーミッション `600`）。api_key は書かない（§6.4 例外を除く）。

### 6.3 資格情報の解決順

1. 環境変数 `ELABFTW_BASE_URL` / `ELABFTW_API_KEY`（CI 用・最優先。`.env` のコミットは避ける）。
   - **base_url と api_key は 1 組で解決する**。env 方式では**両方必須**とし、片方だけ設定されて
     いる場合はエラーにする（別インスタンス用のキーを誤った URL に送る事故を防ぐ）。
2. keyring（api_key）＋ `config.toml`（base_url）。通常の既定。
3. フォールバック（keyring バックエンド無し）: env を第一に促し、不可なら config に api_key 平文＋
   **「平文・600」警告を必ず表示**。

**API キーは例外・デバッグログ・HTTP ログに出さない**（マスクする）。

### 6.4 その他
- 実験ディレクトリ（doc＋図＋データ）に機密は無いので git 管理・共有可。
- keyring 解決不可時は「どのフォールバックを使ったか」を明示（沈黙して平文に落ちない）。

### 6.5 複数プロファイル（複数チーム／インスタンス）＋ 階層設定

Claude の settings.json と同様、**より具体的な層が上書き**する 3 層構成：

| 層 | ファイル | 持つもの |
|---|---|---|
| ① ユーザー | `~/.config/elab/config.toml` | プロファイル群（base_url）、`default_profile`、既定 entity、共通 ignore |
| ② プロジェクト | `<project>/.elab.toml` | プロジェクトの profile 上書き、ignore 追加 |
| ③ ドキュメント | `<dir>/.elab.toml` | その実験だけの上書き |

```toml
# ~/.config/elab/config.toml
default_profile = "labA"

[profiles.labA]
base_url   = "https://elab-a.example.org"
verify_ssl = true
# team = "Team A"   # 任意
[profiles.labB]
base_url   = "https://elab-b.example.org"
```

- api_key はプロファイルごとに keyring（§6.1）。TOML には鍵を書かない。
- 使用プロファイル決定順: frontmatter `profile:` → CLI `--profile` → `default_profile`。
  ただし**送信先（profile / entity / elab_id）について frontmatter と CLI が食い違う場合は、
  黙って優先順位で解決せずエラーで停止する**（誤ったエンティティへの上書き防止）。片方しか
  指定が無ければ通常どおりそれを使う。
- **チーム**: eLab の API キーはアクティブチームに紐づく。チームを使い分けるならチームごとに別
  プロファイル（別 API キー）を作るのを基本とする。`GET /api/v2/users/me` で確認可（§8.2）。
- ユーザーの標準運用は「① だけ」で足りる。②③ は任意上書き。

### 6.6 ローカル状態（base）の保存 — §9.3 と一体

3-way マージの祖先 base（push/pull 後にサーバーから読み戻した本文の全文、§9.1 手順10）は
**`~/.config/elab/state/` に保存**する。プロジェクトには置かない。frontmatter には入れない
（全文でサイズが大きく churn する）。

- **キーは `elab_id` 単独ではなく名前空間化する**: `normalized_base_url + entity種別 + elab_id`。
  `elab_id` だけだと別インスタンス・別 entity 種別で同じ ID が衝突し、誤ったエンティティの base と
  取り違える。normalized は末尾スラッシュ除去等で正規化した base_url。
- base と併せて **`whoami` のアクティブチーム ID（`team`）も記録**する（§9.1 手順3 の誤チーム検出に使う。
  これ以外の状態は持たない）。
- base は **2 形を対で保存**する（§9.1 手順10。実測根拠は §8.2）:
  - **remote-base**: push 成功後にサーバーから `GET` し直した本文（保存・正規化後のリモート形）。
    **リモート変更（Web 編集）検出**と 3-way マージの祖先（逆 transclusion して使用、§9.3）に使う。
    送信文を使わない理由: サーバー正規化により送信文 ≠ 保存文で、毎回 false conflict になるため。
  - **local-base**: push/pull 成功時点の**ローカル本文そのもの**（frontmatter 除去後・transclusion 前の
    ソース形）。**ローカル変更（dirty/clean）判定**（§9.2・§9.5）に使う。remote-base は正規化済みで
    ローカル原文とは恒久に一致しない（§8.2: md モードでも `alt` 付加・`&amp;` 化・末尾改行追記が起き、
    再送しても収束しない）ため、ローカル側の比較は同形の local-base と行う。副次効果として
    ローカル判定はオフラインで完結する。
  - 当初の「remote 形と source 形を二重には持たない」方針は、この実測により**撤回**した
    （片形だけでは local↔base 比較が恒常 dirty になる）。
- state ディレクトリは実験ノート本文そのものを含むため、**ディレクトリ `700` / ファイル `600`** で保護する。

---

## 7. 推奨ローカル構成（運用ガイド）

ツールは「1 ドキュメント＋その隣のファイル」を扱う。推奨は eLabFTW 用の専用ディレクトリを 1 つ持ち、
実験ごとに日付ディレクトリを切る：

```
~/elab/
  260806_esync_test/
    report.md          # frontmatter に elab_id / title
    fig1.png           # <img src="fig1.png"> → 自動アップロード＋URL置換
    data.csv           # [data.csv](data.csv) → 自動アップロード＋URL置換
  260807_next_exp/
    report.md
```

1 ディレクトリ = 1 実験 = 1 エンティティ。載せる図・要約データは実験ディレクトリにコピーして凍結、
重い生データはプロジェクト側に残す運用を推奨（記録の再現性）。

---

## 8. eLabFTW API v2 リファレンス（実装に必要な最小＋確定事実）

ベース URL は `{base_url}/api/v2`。根拠は `elab-doc-sync/client.py` と eLabFTW 公式（下記）に加え、
**2026-08-07 に demo.elabftw.net（5.6.12）で実測確認**した事実（「実測」と明記した箇所）。

### 8.1 認証
- HTTP ヘッダ **`Authorization: <api_key>`**（接頭辞なしの生キー）。JSON 送信時は
  `Content-Type: application/json` を併用。

### 8.2 エンティティ取得・作成・更新・ユーザー
- 取得: `GET /api/v2/{entity}/{id}`。`{entity}` は `experiments` | `items`。
  - **`body`（保存された生のまま：md or html）と `body_html`（常にレンダリング済み HTML）の両方を返す**。
    → pull は `body`（`markdownify` を経ない生ソース）を回収でき、`elab-doc-sync` の往復劣化を避けられる（§9.2）。
  - **ただし完全な verbatim ではない（実測）**: サーバーは保存時に本文中の HTML を正規化する。
    `<img src="fig1.png" width="80%">` は `alt="fig1.png"` を自動付加され、`</figure>` 前の改行が
    除去された（HTMLPurifier 由来と思われる）。**送信した本文 ≠ 保存された本文**。これは §9.3 の
    競合検出に直結する（base は「送信文」ではなく「保存後にサーバーから読み戻した文」にする、§9.1・§6.6）。
  - **正規化の詳細（実測 2026-08-07, demo 5.6.12, `content_type:2` で確認）**:
    - 正規化は **markdown モードでも起きる**（HTML エンティティのみの話ではない）。
    - 属性内 URL の `&` は **`&amp;` にエスケープ**される → 逆 transclusion は URL 解析前に
      HTML unescape が必要（§3.4）。`alt` 未指定の `<img>` には src 由来の `alt` が自動付加される。
    - **保存のたびに本文末尾へ改行が 1 つ追記され、正規化済み本文を再送しても収束しない**
      （3 往復で `\n`→`\n\n`→`\n\n\n` を確認）。「正規化形をローカルに取り込んで一致させる」戦略は
      成立しない（local-base 分離の根拠、§6.6）。diff 表示では末尾改行数の差を無視してよい（§9.5）。
    - 保存と保存の間の `GET` は**安定**（同一本文が返る）。remote-base との比較はこの安定性に依拠する。
    - **CRLF は保存時に LF へ正規化**される（`\r\n` 送信 → `\n` で返る）。local↔local-base 比較は
      両方ローカル形なので影響しないが、ローカル↔リモートの diff（§9.5）は改行コード非依存で比較する。
    - `content_type` は **body のみの PATCH では巻き戻らない**（2 のまま維持。毎 push で 2 を送る現行
      方針と push 後検証で十分）。
- 作成: `POST /api/v2/{entity}`（本文空で作成）。**ID はサーバーが採番**し、`Location` ヘッダ末尾
  （例 `/api/v2/experiments/42`）または JSON `id` で返る。elab はこれを frontmatter の `elab_id`
  として確定させる（§9.1 手順6）。**`elab_id` はクライアントが生成せず、常にサーバー採番値**。
- 更新: `PATCH /api/v2/{entity}/{id}`（JSON。`title`/`body`/`content_type` 等）。
- ユーザー: `GET /api/v2/users/me`（`team`＝アクティブチーム ID、`teams`）。

### 8.3 content_type と既知バグ #6416（重要）
- `content_type`: **`1 = HTML`, `2 = Markdown`**（[apidoc v2](https://github.com/elabftw/elabftw/blob/master/apidoc/v2/README.md)）。
- **既知バグ #6416（5.3.11 で報告）**: 当時は API で `content_type:2` を送っても無視され、
  エンティティのモードは**アカウント設定 `use_markdown` に従っていた**（[issue #6416](https://github.com/elabftw/elabftw/issues/6416)）。
  バグは**作成（POST）時のデフォルト設定**（`AbstractEntity.php` で `use_markdown` を優先していた）。
- **修正済み**: issue は 2026-02-04 のコミット `1ef3de57` でクローズ。**5.5.3（2026-03-16 リリース）は
  この修正コミットを含む**（`git compare` で `behind_by:0` を確認）。よって **5.5.3 以降は `content_type:2`
  が尊重される**。5.3.11〜5.5.2 の旧版のみ影響。
- **実測（2026-08-07, demo.elabftw.net, 5.6.12）**: `use_markdown:0`（HTML エディタ既定）のアカウントでも、
  `PATCH` で `content_type:2` を送ると**エンティティの `content_type` が 2 として保持され、markdown が
  描画された**（`#`→`<h1>`、`**`→`<strong>`、リスト→`<ul>`）。**5.6.12 では #6416 は解消**しており、
  API からモードを設定できる。
  → 方針: `elab` は `content_type:2` を送り、**push 後に `content_type` が 2 であることを確認**する。
  古い（バグの残る）インスタンスでは 2 にならないので、その場合のみ中断して案内する（§9.1）。
  対象インスタンスのバージョンにより挙動が違うため、これは実インスタンスでも要確認（§10.2）。
- **MathJax は md/html 両モードで有効**（inline `$`、block `$$`、AMS 拡張。
  [issue #892](https://github.com/elabftw/elabftw/issues/892)）。実測でも `$\eta = 1 - e^{-kt}$` は
  `body`/`body_html` で素通り保持され、ブラウザ側で MathJax 描画される。

### 8.4 ファイルのアップロード（実測: demo.elabftw.net 5.6.12）
- `POST /api/v2/{entity}/{id}/uploads`、**multipart/form-data**（フィールド `file`、任意 `comment`）。
  このリクエストは `Content-Type: application/json` を付けない（`Authorization` のみ）。成功で 201、
  `Location: .../uploads/{id}` を返す。
- 一覧: `GET /api/v2/{entity}/{id}/uploads` → 各要素は次を含む（実測値の例）:
  - `real_name`: `"data.csv"`（= アップロード時の basename。**日本語・スペース・括弧もそのまま保持**
    される〔実測: `図 1 (テスト).png` が verbatim で往復〕。download URL の `name=` に入れる際の
    percent-encode は必須）
  - `long_name`: `"6f/6f177360-...-...csv"` … **サブディレクトリ分割のため `/` を含む**。download URL の
    クエリ `f=` に入れる際は **percent-encode 必須**（§3.3）。
  - `storage`: `1`、`filesize`: `18`
  - `hash`: `"12cd...bf2"`、`hash_algorithm`: `"sha256"` … **比較可能な sha256 を返す**。ローカルを
    `sha256` でハッシュした値と**完全一致した**（§9.4 のハッシュ判定が成立）。
- **本文に埋め込む表示 URL**: `{base_url}/app/download.php?f={long_name}&name={real_name}&storage={storage}`。
  これは**ブラウザ（Cookie セッション）で見るユーザー向け**。**API キー（`Authorization` ヘッダ）では
  認証されない**（実測: キー有無に関わらずログインページ HTML が返る）。したがって body には埋め込むが、
  elab 自身のダウンロードには使えない。
- **pull で elab 自身がファイルを取得する用**: `GET /api/v2/{entity}/{id}/uploads/{upload_id}?format=binary`。
  こちらは**API キーで実バイナリを返す**（実測: `text/csv` で中身 `col1,col2\n...` を取得）。§9.2 の
  逆 transclusion のダウンロードはこちらを使う。
- 削除: `DELETE /api/v2/{entity}/{id}/uploads/{upload_id}`（push では使わない、§9.4）。

### 8.5 タグ・カテゴリ・メタデータ（実測: demo.elabftw.net 5.6.12）
- タグ追加: `POST /api/v2/{entity}/{id}/tags`（JSON `{"tag": "<name>"}`。既存未存在は自動生成）。
  実測で 201、GET すると `"tags":"CRISPR"`（複数はカンマ結合文字列）／`"tags_id":"50"` が返る。
  **追加のみ**を採用（削除は Web UI、§5）。
- メタデータ: `PATCH` で `{"metadata": "<JSON文字列>"}`。
- カテゴリ: `PATCH` で `category`（ID）。名前→ID 解決は
  `GET /api/v2/teams/{team_id}/experiments_categories`（items は対応する types 系）で `{id, title}` の
  一覧を取り、`title` 一致で ID を得る（実測: `{"id":13,"title":"Synthesis"}` 等）。自動作成はしない。

### 8.6 リビジョン（本文の版履歴）
- **かつては API v2 に revision の GET が無かった**（[discussion #5221](https://github.com/elabftw/elabftw/discussions/5221)、
  古い openapi）。その前提で「base はサーバーから取れない」としていた。
- **実測で訂正（demo.elabftw.net 5.6.12）**: **リビジョンは API で取得できる**。
  - 一覧: `GET /api/v2/{entity}/{id}/revisions` → `[{id, content_type, created_at, fullname}, …]`
  - 個別: `GET /api/v2/{entity}/{id}/revisions/{rev_id}` → **その版の `body` を返す**（実測で過去本文を回収）。
- **それでも base はローカル（§6.6）に持つ**。理由は「取れないから」ではなく、**どのリビジョンが自分の
  前回 push に対応するかを紐づける情報が無い**（ambiguous ancestor）ため、3-way マージの祖先 base を
  リビジョンから自動復元できないから。局所 base は依然として正しい。リビジョンは**読み取り専用の
  補助**（履歴の閲覧・手動マージ時の比較材料）として使える（§9.3）。
- eLab はサーバー側に履歴を保持しており、**万一の上書きは Web UI から復元できることが多い**（安全網）。
  ただし**リビジョンは保存のたびに作られるわけではない**（実測: 5 回の PATCH に対し 2 件のみ。
  eLabFTW は差分量・間隔の閾値でリビジョン作成を間引くため）。**直近の全 push が必ず残る保証はない**ので、
  安全網は「最後の砦」であって競合検出（§9.3）の代替にはならない。
- 5.5.3 で revisions GET が有効かは実インスタンスで要確認（§10.2）。

---

## 9. 同期セマンティクス

**順序が重要**: 参照ファイルを先にアップロードしてから作成・競合検出をすると、(a) 新規は
アップロード先の ID がまだ無い、(b) 既存はアップロード後に競合が見つかっても本文は更新されず
添付だけ増える、という破綻が起きる。そこで**副作用のある操作を最後にまとめる**次の順序にする。

1. **検証（副作用なし）**: ドキュメントを読み、フロントマターを分離（本文には含めない）。
   §6.5 の送信先解決（不一致はエラー停止）。
2. **アップロード計画のみ作る（まだ変更しない）**: `.elabignore` を考慮して §3 の対象判定を行い、
   「どのローカルファイルを上げ、どのパスをどの URL に置換するか」を計画する。この時点では
   アップロードも置換もしない。
   - **`.elabignore`**: `.gitignore` 同様の除外パターン（層設定で追加可、§6.5）。
   - **「今回アップロードするファイル一覧」を常に表示**（git status 風の可視化）。
3. **認証・チーム確認**: `whoami`（§8.2）で認証と**アクティブチーム ID が state に記録した値と一致**する
   ことを確認（記録は §6.6。初回 push で記録し、以後の不一致は「別チームのキーで誤った先に push しかけて
   いる」兆候としてエラー停止）。state に記録が無ければ確認をスキップし、今回の値を手順10で記録する。モード（`content_type`）は 5.6.12 では PATCH で設定できるので、事前中断はせず**PATCH 後に
   検証**する（手順10）。
4. **競合検出（既存エンティティのみ）**: §9.3。リモート本文が remote-base と不一致なら**中断**（Web 編集を消さない）。
   - **base が無い場合（別マシンからの初 push 等）は比較不能なので中断**し、「先に `elab pull` で取得・
     統合してから push」（pull が base を確立する）か、リモートを見ずに上書きしてよいなら `--force` を案内する。
     黙って上書きに進まない。
5. **サイズ確認プロンプト**: 一定サイズ超（実装時に閾値決定）の**新規**アップロードは確認（暴発対策）。
6. **新規なら作成**: `elab_id` が無ければ `POST` で作成する。**ID はサーバーが採番**し（§8.2）、
   その値を**作成直後に frontmatter の `elab_id` へ atomic に書き戻す**（id を失わない、§5）。
   以降この doc は常にその `elab_id` のエンティティに対応する。
7. **アップロード**: 計画に従い参照ファイルをアップロード（重複回避は §9.4）、本文コピーのパスを実 URL に置換。
8. **PATCH 直前の再確認（既存のみ）**: 手順4以降に Web 編集が入っていないか、リモート本文が base と
   一致することを再確認。不一致なら中断。
9. **本文・メタデータ送信**: 本文を **`content_type:2`（生 Markdown）** で `PATCH`。**md→html 変換も
   数式保護もしない**（eLab が Markdown＋MathJax＋生 HTML `<figure>` を描画）。`title`/`category` を
   反映し、`tags` は追加のみ（§8.5）。
10. **モード検証＋base 保存**: 送信成功後、**サーバーから `GET` し直す**。まず `content_type` が 2
    （markdown）であることを確認し、2 でなければ（#6416 が残る旧インスタンス）警告して
    「eLab 設定で markdown エディタを有効に」と案内する。次に base を **2 形で** `~/.config/elab/state/`
    （名前空間化キー、§6.6）に保存する: **`GET` した本文を remote-base**（送信文は使わない。サーバー
    正規化により送信文を base にすると次回の競合検出で毎回 false conflict になるため）、**手順1で得た
    ローカル本文（frontmatter 除去後・transclusion 前）を local-base** として保存。この 1 回の `GET` で
    「モード検証」と「次回 `GET` と同形の remote-base 確定」を兼ねる。

**副作用ゼロの規範**: **手順6より前の中断**（`--dry-run`・手順4の競合・確認拒否）では、**リモートにも
ローカルにも副作用を残さない**（アップロード・PATCH・frontmatter 書き戻し・base 保存を一切行わない）。
**手順8で中断した場合はアップロード済みの添付が残る**（本文は未変更。添付は §9.4 の重複回避で次回
push 時に再利用されるため実害は小さいが、ゼロではないことを正直に明記する）。

- **`--dry-run`**: 手順7以降を実行せず、アップロード一覧と置換予定を提示する。新規アップロードは
  `long_name`/`storage` がアップロード後にしか判らないため、実 URL の代わりに
  `UPLOAD_PENDING:<basename>` のようなプレースホルダで表示する（既存再利用分は実 URL を表示できる）。
  - **立ち位置**: これは **push アクションの完全予行**（アップロード一覧＋パス→URL 置換プラン＋
    競合/モードによる中断条件まで、実際の push フローで判定）。**状態の一望だけなら `status`（§9.5）**を
    使う。`status` はサマリ、`--dry-run` は詳細な置換プレビューまで、と棲み分ける。

### 9.2 pull

- `GET` でエンティティを取得し、**`body`（生 Markdown ソース）を回収**（`markdownify` を経ないので
  `elab-doc-sync` のような往復劣化はない。ただしサーバーの HTML 正規化は入る、§8.2）。
- §3.4 の**逆 transclusion**で、この実体のアップロード URL をローカル basename パスに戻し、画像等を
  ローカルへ配置（ダウンロードは API の `?format=binary`、§3.4・§8.4）→ 本文をソース形にする。
  - **配置ファイル名は `real_name` の basename のみを採用**し（パス区切り・`..`・先頭 `~` を除去）、
    **ドキュメントディレクトリ配下にのみ書き込む**。`real_name` は Web UI で改名されうる外部入力なので、
    §3.2 と対になる逆方向のパス安全境界として扱う。
  - **ダウンロードした実体の配置時、同名ローカルファイルが既にあり内容が異なる場合は上書きしない**。
    バイト比較で差があれば `<name>.remote` として退避し、競合として扱う（ローカルの実体を消さない）。
- **clean（ローカルに未 push 変更が無い）なら `report.md` を直接更新**する。clean 判定は
  **現在のローカル本文 ↔ local-base** の比較（同形同士、§6.6）。更新後、**取得した本文を remote-base、
  書き込んだソース形本文を local-base** として保存（以後の競合検出の基準を pull 後の状態に更新する）。
- ローカルに未 push 変更があり衝突する場合は §9.3（勝手に上書きしない）。
- サイドカーが要る場合の命名は `<doc名>.remote.md`。

### 9.3 競合検出とマージ（git 委譲）

**目的**: ユーザーが Web UI で編集することがある。push の全文上書きで Web 側編集を消さない。

1. push/pull の前に `GET` で現在のリモート本文を取得し、remote-base（前回 push/pull 後にサーバーから
   読み戻した本文、§6.6）と比較。**どちらも「サーバー保存形」なので正規化差で誤検出しない**（§8.2）。
   - **一致** → 前回 push 以降に Web 編集なし → そのまま進行。
   - **不一致** → リモートが変更された → **中断**して警告。
2. **マージはツールで実装せず git に委譲する**（Q10 の決定）。ツールは以下をソース形で提供：
   - remote-base（逆 transclusion 済み）と remote（逆 transclusion 済み）をファイルに出す
     （両者は同形なので、その差分＝Web 編集そのもの。ローカル↔remote-base 間の正規化ノイズは
     ours 側の変更として扱われ、ローカル優先で解決される）。
   - ユーザーが普段の git マージツール（`git merge-file` 等）で `report.md` に統合。
3. **base がそのマシンに無い場合**（別マシンから初操作等）は 3-way に落とせないので、**2-way に
   グレースフル縮退**（remote を出して「ローカルと見比べて」）。git 委譲が自然にこの形になる。
   - 補助（任意）: リビジョン API（§8.6）が使えるなら、**過去版の一覧・本文を読み取り専用で提示**でき、
     ユーザーの手動マージの比較材料になる。ただし「どの版が祖先か」は自動判定しない（ambiguous ancestor）。
4. `--force` は競合を無視して強制上書き（Web 側変更は失われる）。
5. **安全網**: 万一潰しても eLab のサーバー履歴から Web UI で復元可能（§8.6）。

### 9.4 冪等性（添付の重複防止）

1. アップロード前に `GET .../uploads` を引き `real_name`（= basename）で既存検索。
   **同名エントリは複数ヒットしうる**（実測: 同名を再 POST すると旧エントリはアーカイブされず
   両方 `state:1` で共存する）。複数ヒット時は**全エントリとハッシュ照合し、一致したものを再利用**する。
   どれとも一致しなければ新規アップロードし、**新エントリの URL を使う**（旧エントリは残す。削除しない
   のは 3 のとおり）。
2. 同一判定は**リモートが返すハッシュを最優先**する。既存があり、`uploads` 応答に `hash`/`sha256`
   （とアルゴリズム）が含まれるなら、**ローカルを同じアルゴリズムでハッシュして照合**し、一致すれば
   再アップロードせず既存 URL を再利用する。ハッシュは**都度計算で照合するだけで state には保存しない**
   （attachment_map は持たない、§3.4）。
   - リモートがハッシュを返さない／信用できない場合のみ、**同名かつ同サイズ**をフォールバック判定に使う。
   - サイズのみ判定は「同じ長さで中身だけ変わる」ファイル（CSV 等）を取り逃す穴があるため、
     ハッシュが取れる限りハッシュを使う。**5.6.12 では `uploads` が `hash`＋`hash_algorithm:"sha256"`
     を返し、ローカル `sha256` と一致することを実測確認済み**（§8.4）。
3. **push 中に古いアップロードを自動削除しない**。過去リビジョン内の URL が切れうるし、
   「Web 履歴が安全網」（§8.6）という前提と矛盾する。不要添付の掃除は将来の別コマンドに分離する
   （MVP では実装しない）。
4. **別実体の basename 衝突はエラー**にする（§3.5 の警告より強く扱う）。同名で内容の異なる
   ローカルファイルを 1 エンティティに上げようとした場合、どちらの URL に置換すべきか決まらず
   誤リンクになるため、push を中断してユーザーに解消を促す。
5. `--force` は **§9.3 の競合上書き専用**（Web 側変更を無視して本文を上書きする）。添付の再アップロード
   とは無関係（`--force` で添付を上書き・削除はしない）。

### 9.5 status / diff（副作用なしの読み取り系）

git 的中核 UX。**どちらも送信・アップロード・frontmatter 書き戻し・base 保存をしない**（純粋な可視化）。
新しい同期セマンティクスは持ち込まず、**既存機構の"副作用なし版"**として実装する。

> **`push --dry-run`（§9.1）との棲み分け**: `status` は**状態のサマリ**（clean/dirty・リモート変更の
> 有無・アップロード件数・モード）を一望する健康診断。**パス→URL の置換プラン（変換後に何が送られるか）
> の詳細プレビューは `push --dry-run`** が担う（その push の完全予行）。役割: `status`＝どこにいるか、
> `diff`＝何が変わったか、`push --dry-run`＝push したら何が起きるか。

**`elab status`**（= §9.1 preflight の表示だけ版）:

- **ローカル状態**: 現在の本文（ソース形）と **local-base**（§6.6）を比較し、「ローカルに未 push 変更
  あり/なし（clean）」。同形同士の厳密比較で、**ネットワーク不要**（オフラインでも判定できる）。
- **リモート状態**: `GET` した現在のリモート本文と **remote-base** を比較し、「Web 編集あり/なし」（§9.3 の判定と同じ）。
- **アップロード計画**: §3 の対象判定で「今回上げる/再利用するファイル一覧」（§9.1 手順2 と同じ、実行はしない）。
- **モード**: 対象エンティティの `content_type`（markdown か）。
- base がそのマシンに無ければ「base 無し（比較不可）」と明示。

**`elab diff [--base]`**（= §9.3 が競合時に作る比較を、競合を待たずオンデマンドで）:

- 既定は **ローカル ↔ リモート**。`--base` で **ローカル ↔ local-base**（「前回 push から自分が何を
  変えたか」。同形なので正規化ノイズゼロ）。
- ローカル ↔ リモートでは**リモート側を逆 transclusion してソース形に揃えてから** diff する
  （URL とローカルパスが混在しないように、§3.4）。サーバー正規化（§8.2）による差（`alt` 付加・
  `&amp;` 化）は残るため、この diff にはノイズが混ざりうると出力時に注記する。**末尾改行数の差と
  改行コード差（CRLF/LF）は無視する**（サーバーが保存ごとに末尾改行を追記し、CRLF を LF に正規化
  するため、§8.2）。
- 出力は unified diff 相当（実装は `difflib` 等）。**送信はしない**。

> **将来（MVP 外）**: リビジョン履歴を辿る `elab log` / `elab show <rev>` は、revisions API（§8.6）を使った
> 読み取り専用機能として後で足せる。頻度が低く対象インスタンスの revisions 有無にも依存するため MVP 外。

---

## 10. 決定事項と実装時確認

### 10.1 確定した決定（設計セッションで確定）
1. **名前 / コマンド**: `elab`（ユーザー向け同期 CLI。Markdown 専用ではない。旧称 `elabmd`）。
2. **中核**: 参照した実在ローカルパスを記法・種別問わず upload＋実 URL 置換。添付＝ローカルパス／
   他ノートへのリンク＝eLab URL（§3）。
3. **本文形式**: 生 Markdown 送信（`content_type:2`）。変換・数式保護なし（§9.1）。**5.6.12 では
   API がエンティティの `content_type` を 2 に設定でき（#6416 解消・実測）、`use_markdown:0` でも
   markdown 描画**。push 後に `content_type` が 2 でなければ（バグの残る旧版）中断する（§8.3・§9.1）。
4. **図・数式**: `<figure>` は生 HTML のまま、`$...$`/`$$...$$` はそのまま。eLab が描画。**実測（5.6.12）で
   `<figure>` は `body_html` に生 HTML で保持され、`$...$` も素通り**。実インスタンスでも最終確認（§10.2）。
5. **pull**: 逆 transclusion 付き。clean なら `report.md` 直接更新（§9.2）。
6. **競合/マージ**: base を `~/.config/elab/state/`（キー=`base_url+entity種別+elab_id` の名前空間、
   §6.6）に **remote-base / local-base の 2 形で**保存し、**マージは git に委譲**。base 無しの pull は
   2-way 縮退、base 無しの既存エンティティへの push は中断（pull か `--force` を案内、§9.1 手順4）。
   eLab 履歴が復元の保険（§9.3）。
   リビジョンは API で取得可能だが、前回 push との対応が不明（ambiguous ancestor）なので base の
   自動復元には使わず、局所 base を主とする（§8.6）。
7. **暴発対策**: `.elabignore`＋push 時アップロード一覧表示＋大容量新規は確認（§9.1）。
8. **認証**: keyring（OS 保管庫）＋ base_url は TOML＋env 上書き。機密はプロジェクトに置かない（§6）。
9. **プロファイル/設定**: 複数チーム対応。settings.json 風の 3 層設定（§6.5）。
10. **メタ配置**: frontmatter＝elab_id＋title/tags/category（+任意 profile）。base＝state
    （名前空間化キー、remote/local の 2 形＋チーム ID、§6.6）。
    添付の重複判定はリモート hash 優先・無ければサイズ（hash は保存せず都度照合）、push 中の添付
    自動削除はしない（§5・§6.6・§9.4）。
11. **ファイル名**: 固定しない（既定 `report.md`、任意名可、サイドカー `<名前>.remote.md`）。
12. **既定エンティティ種別**: `experiments`（frontmatter で上書き可）。
13. **status / diff は MVP**: 副作用なしの読み取り系（`status`＝preflight 表示、`diff`＝ソース形比較）。
    git 的中核 UX で、既存機構の再利用で安く作れる。`log`/`show`（リビジョン閲覧）は MVP 外（§9.5）。

### 10.2 実装時に確認・確定すべき事項

**demo.elabftw.net 5.6.12 で実測確認済み（2026-08-07）**:

| 項目 | 結果 | 反映先 |
|---|---|---|
| `content_type:2` が効くか（#6416） | **解消**。`use_markdown:0` でも 2 が保持され markdown 描画 | §8.3・§9.1 |
| `<figure>`・`$...$` の保持 | `body_html` に生 HTML／数式素通りで保持 | §8.3・§10.1-4 |
| `body` の往復 | 生ソース回収可。ただし**サーバーが HTML を正規化**（`alt` 付加・改行除去・`&amp;` 化）。**md モードでも発生**。**保存ごとに末尾改行を追記し再送しても収束しない**。保存間の GET は安定 | §8.2・§9.1・§9.3・§6.6 |
| uploads のハッシュ | `hash`＋`hash_algorithm:"sha256"` を返し、ローカル sha256 と一致 | §9.4 |
| `long_name` の形 | `/` を含む（percent-encode 必須） | §3.3・§8.4 |
| download.php の認証 | **API キーでは不可**（ログイン画面）。pull は `?format=binary` API を使う | §3.4・§8.4・§9.2 |
| elab_id 採番 | POST が `Location` で採番 ID を返す | §8.2 |
| カテゴリ解決 | `GET /teams/{id}/experiments_categories` の `{id,title}` を title 一致で解決 | §8.5 |
| タグ | POST で追加、`tags`/`tags_id` で反映（追加のみ採用） | §8.5 |
| リビジョン取得 | `GET .../revisions` 一覧・`GET .../revisions/{id}` 本文とも**取得可**（旧前提と逆） | §8.6・§9.3 |
| リビジョン作成頻度 | **毎保存ではない**（5 PATCH で 2 件。閾値で間引かれる） | §8.6 |
| 同名再アップロード | 旧エントリは残り**両方 `state:1` で共存**（自動アーカイブなし） | §9.4 |
| 日本語ファイル名 | `real_name` に verbatim 保持（`図 1 (テスト).png`）。binary API で往復可 | §8.4 |
| CRLF | 保存時に **LF へ正規化** | §8.2・§9.5 |
| `content_type` の維持 | body のみの PATCH では **2 のまま巻き戻らない** | §8.2・§8.3 |

**ユーザーの実インスタンス = eLabFTW 5.5.3**:
- **#6416 は解消済み**（5.5.3 は修正コミット `1ef3de57` を含む、§8.3）。`content_type:2` は効くと判断してよい。
  ダメ押しで、実インスタンスで 1 度だけ「PATCH 後に `content_type:2` が保持される」ことを確認すれば十分。
- **`<figure>` 描画**: markdown モードでの実描画は render 依存なので、実インスタンスで 1 度目視確認
  （デモ 5.6.12 では `body_html` に生 HTML 保持を確認済み。5.5.3 でも同経路と推定）。
- **タグ削除 API**: MVP は追加のみ。将来削除するなら `DELETE .../tags/{id}` 等の可否。
- **リビジョン GET**: デモ 5.6.12 で取得可を確認。5.5.3 でも `GET .../revisions` が使えるかは、
  §9.3 の読み取り専用補助を実装する場合のみ確認すればよい（MVP のコア動作には不要）。

**実装時に自分で決める（インスタンス非依存）**:
3. **大容量アップロードの確認閾値**（例 25MB）と `.elabignore` の記法。
4. **HTML 書換の範囲**: `src=`/`href=` のみ（MVP）。`srcset` / CSS `url()` は対象外で警告のみ。

---

## 11. 実装方針

- **言語/形態**: Python 3.10+、`uv tool` インストール可能なパッケージ。**名前は全て `elab` で統一**:
  pyproject `name = "elab"`、`[project.scripts]` に `elab`、import パッケージも `elab`
  （`uv tool install elab` → コマンド `elab`）。ドットファイル `.elabignore` / `.elab.toml`、
  設定 `~/.config/elab/`、keyring サービス `"elab"` も同様（接続先サービスの `eLabFTW` とは別概念）。
  依存は最小（`requests`、`keyring`〔§6〕、逆 transclusion 用に `elab-doc-sync` の手法を参照。
  TOML 読取は 3.11+ の `tomllib`／3.10 は `tomli`、書込は自前 or `tomli-w`）。
- **CLI**: `argparse`。サブコマンドは §4。
- **構造の目安**: `client.py`（API 薄いラッパ）/ `transclude.py`（§3 双方向）/ `config.py`（§6）/
  `state.py`（§6.6 base）/ `sync.py`（§9）/ `cli.py`。
- **テスト**: §3 のスキャン・置換・逆 transclusion はネットワーク非依存でユニットテスト
  （各記法、http 除外、実在判定、複数参照の単一化、basename 衝突がエラーになること、
  コードフェンス／インラインコード／HTML コメント内が解析されないこと、ドキュメント配下外
  （絶対パス・`..`・シンボリックリンク脱出）が弾かれること、percent-encode を含む URL の生成と逆解析、
  フラット構成での basename 往復、置換がスパン基準でありフェンス内・部分一致文字列を巻き込まないこと、
  pull 配置時の `real_name` サニタイズ（§9.2））。API 層はモック。
- **進め方**: 本仕様確定後、実装は Codex（`codex exec`）に委譲し、`git diff`＋テストでレビューする。

---

## 付録 A. `elab-doc-sync` 参照情報
- リポジトリ: <https://github.com/Kosaku-Noba/elab-doc-sync>（試用時 0.4.2）。
- 導入先: `~/.local/share/uv/tools/elab-doc-sync/lib/python3.14/site-packages/elab_doc_sync/`。
- 参照実装: `sync.py`（`IMAGE_RE`, `_rewrite_images`, `_download_images`=逆 transclusion,
  `_md_to_html`, `_sync_attachments`）、`client.py`（`ELabFTWClient`, `upload_file`, `list_uploads`）。

## 付録 B. 参考リンク
- eLabFTW API: <https://doc.elabftw.net/api.html> / <https://doc.elabftw.net/api/elabapi-html/>
- content_type バグ: <https://github.com/elabftw/elabftw/issues/6416>
- リビジョン非公開の経緯: <https://github.com/elabftw/elabftw/discussions/5221>
- Markdown で数式: <https://github.com/elabftw/elabftw/issues/892>
- eLabFTW changelog: <https://doc.elabftw.net/changelog.html>

## 付録 C. 互換性確認用の公開デモインスタンス
- **URL**: <https://demo.elabftw.net/>（ログインは `https://demo.elabftw.net/login.php`）。**公開・定期リセット
  される使い捨て環境**。API の形（エンドポイント・返却フィールド・往復挙動）の確認に使える。
- API キーは Web UI（Settings → API keys）で発行。デモは使い捨てなので実質的な機密ではない。
- **本仕様の「実測（demo 5.6.12）」記述はこの環境で確認**（§8.2〜8.5・§9.4）。バージョン依存の挙動
  （#6416 等）は、対象の実インスタンス版でも確認すること（§10.2）。デモの版はリセットで上がりうる。
- **注意**: 版依存の挙動をデモで確認しても、実インスタンスが別版なら結果が異なりうる（デモは最新版寄り）。
