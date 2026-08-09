# eLabFTW API v2 — Measured Reference (observation log)

- **Nature**: a living reference. It records **observations of the external world that is eLabFTW**. Re-acquiring
  these facts requires hitting a live server (expensive), so they are kept as settled findings. **Update it only
  when the target instance's version changes.**
- **What it is not**: not elab's own design rationale (that is [DESIGN.md](DESIGN.md)); not the behavioral contract
  (that is the **tests**). It holds only "how eLabFTW actually behaves."
- **Origin**: §8, §10.2, Appendix B, and Appendix C of the former `docs/SPEC.md`.
- **Observation environment**: unless noted, "measured" means confirmed on **demo.elabftw.net 5.6.12
  (2026-08-07..08)**. Base URL is `{base_url}/api/v2`. **Re-confirm version-dependent behavior on the target
  instance's own version** (see § Version-specific).

---

## Authentication

- HTTP header **`Authorization: <api_key>`** (raw key, no prefix). When sending JSON, also set
  `Content-Type: application/json`.

## Get / create / update entities and user

- Get: `GET /api/v2/{entity}/{id}`. `{entity}` is `experiments` | `items`.
  - **Returns both `body` (stored as saved: md or html) and `body_html` (always rendered HTML)** → pull can
    recover `body` (raw source, not run through `markdownify`), avoiding `elab-doc-sync`'s lossy round-trip.
- Create: `POST /api/v2/{entity}` (created with empty body). **The server assigns the ID**, returned at the end of
  the `Location` header (e.g. `/api/v2/experiments/42`) or as JSON `id`. **`elab_id` is never client-generated; it
  is always the server-assigned value.**
- Update: `PATCH /api/v2/{entity}/{id}` (JSON: `title`/`body`/`content_type`, etc.).
- User: `GET /api/v2/users/me` (`team` = active team ID, `teams`).

## Body normalization (the fact most directly tied to conflict detection)

**`body` is not fully verbatim.** The server normalizes the body on save. **What you send ≠ what is stored.** This
is the reason base must be "what the server returns after save," not "what was sent."

Normalization details (measured 2026-08-07/08, demo 5.6.12, `content_type:2`):

- Normalization happens **even in markdown mode** (it is not just an HTML-entities issue).
- `<img src="fig1.png" width="80%">` gets `alt="fig1.png"` auto-added, and the newline before `</figure>` was
  removed (likely HTMLPurifier).
- `&` inside attribute URLs is **escaped to `&amp;`** → reverse transclusion must **HTML-unescape** before parsing
  URLs.
- **One newline is appended to the end of the body on every save, and re-sending the normalized body does not
  converge** (observed `\n`→`\n\n`→`\n\n\n` over 3 round-trips). The strategy of "pull the normalized form locally
  to make them match" does not hold (the reason local-base is kept separate from remote-base).
- `GET` between saves is **stable** (returns the same body). Comparison against remote-base relies on this
  stability.
- **CRLF is normalized to LF on save** (`\r\n` sent → `\n` returned). Compare local↔remote independently of line
  endings.
- **A Markdown `<URL>` angle-bracket link target is stripped as a tag** (measured 2026-08-08). push should replace
  the whole local `<PATH>` and send a bare URL in the body.
- **This HTML normalization reaches inside code fences too** (measured 2026-08-08: `<figure 1.csv>` inside a fence
  was rewritten to `<figure>` + `</figure>`). The tool cannot prevent it, but remote↔remote-base comparison is
  same-form, so it does not cause false conflicts.
- `content_type` **does not revert on a body-only PATCH** (stays 2).

## content_type and known bug #6416

- `content_type`: **`1 = HTML`, `2 = Markdown`**
  ([apidoc v2](https://github.com/elabftw/elabftw/blob/master/apidoc/v2/README.md)).
- **Known bug #6416 (reported in 5.3.11)**: at the time, sending `content_type:2` via the API was ignored and the
  mode followed the account setting `use_markdown` ([issue #6416](https://github.com/elabftw/elabftw/issues/6416)).
  The bug was in the create (POST) default.
- **Fixed**: closed by commit `1ef3de57` on 2026-02-04. **5.5.3 (2026-03-16) includes this fix.** So **5.5.3+
  respects `content_type:2`.** Only the older 5.3.11..5.5.2 range is affected.
- **Measured (5.6.12)**: even on a `use_markdown:0` account, sending `content_type:2` via `PATCH` kept it **as 2 and
  rendered markdown** (`#`→`<h1>`, etc.).
  → Policy: elab sends `content_type:2` and **verifies `content_type == 2` after push**. On old (still-buggy)
  instances it will not become 2, so abort and advise only in that case.
- **MathJax is enabled in both md/html modes** (inline `$`, block `$$`, AMS extensions.
  [issue #892](https://github.com/elabftw/elabftw/issues/892)). Measured: `$\eta = 1 - e^{-kt}$` passes through
  intact and is rendered client-side.

## File uploads

- `POST /api/v2/{entity}/{id}/uploads`, **multipart/form-data** (field `file`, optional `comment`). This request
  does not set `Content-Type: application/json` (only `Authorization`). 201 on success, `Location:
  .../uploads/{id}`.
- List: `GET /api/v2/{entity}/{id}/uploads` → each element (example measured values):
  - `real_name`: `"data.csv"` (= the upload basename. **Japanese, spaces, and parentheses are preserved verbatim**
    [measured: `図 1 (テスト).png` round-trips verbatim]. percent-encode required when placing into the download
    URL `name=`).
  - `long_name`: `"6f/6f177360-...-...csv"` … **contains `/` due to subdirectory sharding**. percent-encode
    required when placing into the download URL `f=`.
  - `storage`: `1`, `filesize`: `18`
  - `hash`: `"12cd...bf2"`, `hash_algorithm`: `"sha256"` … **a comparable sha256**. It **matched exactly** a local
    sha256 (usable for dedup).
- **Display URL embedded in the body**: `{base_url}/app/download.php?f={long_name}&name={real_name}&storage={storage}`.
  This is **for a user viewing in a browser (Cookie session)**. It is **not authenticated by the API key
  (`Authorization`)** (measured: with or without the key, a login-page HTML is returned). Embed it in the body, but
  elab cannot use it for its own downloads.
- **For elab's own file retrieval during pull**: `GET /api/v2/{entity}/{id}/uploads/{upload_id}?format=binary`.
  This **returns the real binary with the API key** (measured: got `text/csv` content). Reverse transclusion
  downloads use this.
- Delete: `DELETE /api/v2/{entity}/{id}/uploads/{upload_id}` (not used by push).

## Tags / categories / metadata

- Add tag: `POST /api/v2/{entity}/{id}/tags` (JSON `{"tag": "<name>"}`; a non-existent one is auto-created).
  Measured 201; GET returns `"tags":"CRISPR"` (multiple joined by commas) / `"tags_id":"50"`. **Add-only** (delete
  via Web UI).
- Metadata: `PATCH` with `{"metadata": "<JSON string>"}`.
- Category: `PATCH` with `category` (ID). Resolve name→ID via
  `GET /api/v2/teams/{team_id}/experiments_categories` (items use the corresponding types endpoint), reading the
  `{id, title}` list and matching on `title` (measured: `{"id":13,"title":"Synthesis"}`, etc.). No auto-creation.

## Revisions (body version history)

- **Retrievable, as measured** (opposite of the old assumption):
  - List: `GET /api/v2/{entity}/{id}/revisions` → `[{id, content_type, created_at, fullname}, …]`
  - Individual: `GET /api/v2/{entity}/{id}/revisions/{rev_id}` → **returns that version's `body`**.
- **base is still kept locally.** Not because it can't be fetched, but because there is **no information tying a
  revision to "my last push"** (ambiguous ancestor), so the 3-way merge ancestor cannot be auto-restored from
  revisions. Revisions are a **read-only aid** (browsing history, comparison material for manual merges).
- **Revisions are not created on every save** (measured: only 2 for 5 PATCHes; thinned by diff-size/interval
  thresholds). There is **no guarantee every recent push survives**, so Web-UI recovery is a "last resort," not a
  substitute for conflict detection.

---

## Measured summary (demo.elabftw.net 5.6.12, 2026-08-07/08)

| Item | Result |
|---|---|
| Does `content_type:2` work (#6416) | **Resolved.** Kept as 2 and rendered markdown even with `use_markdown:0` |
| `<figure>` / `$...$` preservation | Kept as raw HTML in `body_html` / math passes through |
| `body` round-trip | Raw source recoverable, but the **server normalizes** (`alt` added, newline removed, `&amp;`). **Happens in md mode too.** **Appends a trailing newline each save; re-sending does not converge.** GET between saves is stable |
| upload hash | Returns `hash` + `hash_algorithm:"sha256"`, matches local sha256 |
| `long_name` shape | Contains `/` (percent-encode required) |
| download.php auth | **Not possible with the API key** (login page). pull uses the `?format=binary` API |
| elab_id assignment | POST returns the assigned ID via `Location` |
| category resolution | Resolve `{id,title}` from `GET /teams/{id}/experiments_categories` by title match |
| tags | Added via POST, reflected in `tags`/`tags_id` (add-only) |
| revision retrieval | Both list and individual body are **retrievable** (opposite of old assumption) |
| revision creation frequency | **Not every save** (2 for 5 PATCHes) |
| re-upload of same name | Old entry remains; **both coexist as `state:1`** (no auto-archive) |
| Japanese file names | Preserved verbatim in `real_name` (`図 1 (テスト).png`); round-trips via the binary API |
| CRLF | **Normalized to LF** on save |
| `content_type` persistence | **Stays 2** on a body-only PATCH |

---

## Version-specific / to confirm

**User's real instance = eLabFTW 5.5.3**:

- **#6416 is resolved** (5.5.3 includes fix commit `1ef3de57`). `content_type:2` can be assumed to work. For extra
  assurance, confirm once on the real instance that "`content_type:2` persists after PATCH."
- **`<figure>` rendering**: actual rendering in markdown mode is render-dependent, so eyeball it once on the real
  instance (raw-HTML preservation in `body_html` confirmed on demo 5.6.12; 5.5.3 presumed the same path).
- **Revision GET**: confirmed retrievable on demo 5.6.12. Whether 5.5.3 supports it need only be checked if you
  implement the read-only revision aid (not needed for MVP core behavior).
- **Tag deletion API**: MVP is add-only. If you later delete, confirm feasibility of `DELETE .../tags/{id}` etc.

**Note**: demo tracks recent versions and can be bumped on reset. Confirming version-dependent behavior on demo does
not guarantee the same on the real instance if it runs a different version.

---

## Reference links

- eLabFTW API: <https://doc.elabftw.net/api.html> / <https://doc.elabftw.net/api/elabapi-html/>
- content_type bug: <https://github.com/elabftw/elabftw/issues/6416>
- History of revisions being non-public: <https://github.com/elabftw/elabftw/discussions/5221>
- Math in Markdown: <https://github.com/elabftw/elabftw/issues/892>
- changelog: <https://doc.elabftw.net/changelog.html>

## Public demo for compatibility checks

- **URL**: <https://demo.elabftw.net/> (login `https://demo.elabftw.net/login.php`). A **public, periodically-reset,
  disposable** environment. Useful for confirming API shape (endpoints, returned fields, round-trip behavior).
- Issue an API key via the Web UI (Settings → API keys). Since the demo is disposable, it is not a real secret.
