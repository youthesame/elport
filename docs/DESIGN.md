# elab — Design Record (why it is built this way)

- **Nature**: a frozen document. It captures the **rationale and settled decisions** behind elab. Rarely updated.
- **What it is not**: not a behavioral spec (the **tests** are authoritative for behavior); not a usage guide
  (that is the [README](../README.md)); not a record of measured eLabFTW API facts (that is
  [ELABFTW-API.md](ELABFTW-API.md)).
- **Origin**: inherits §1, §2, §10.1, and Appendix A of the former `docs/SPEC.md` (design sessions of
  2026-08-06..08 plus external review).

---

## 1. Background

### 1.1 Starting point

The user is a researcher who uses eLabFTW as a lab notebook, writing Markdown/HTML in their editor of choice and
syncing it to eLabFTW entries. They first tried the existing OSS `elab-doc-sync` (CLI name `esync`,
<https://github.com/Kosaku-Noba/elab-doc-sync>), but a close reading revealed a design that fundamentally clashes
with the user's writing style, leading to the decision to replace it with a thin, purpose-built tool.

### 1.2 How `elab-doc-sync` actually behaves (facts confirmed by close reading)

Source: `~/.local/share/uv/tools/elab-doc-sync/lib/python3.14/site-packages/elab_doc_sync/`.

1. Image upload targets **only** the Markdown image form `![alt](path)` (`IMAGE_RE`).
2. HTML `<img src>` and ordinary links `[text](path)` are **not** handled (neither uploaded nor URL-rewritten).
3. Non-image attachments are handled by scanning an `attachments_dir` folder (flat, non-recursive; image
   extensions excluded).
4. `attachments_dir` is shared across all entities in a target (even `each` can't vary it per experiment).
5. The `each` file→entity mapping is keyed by basename.
6. It never sends `content_type` (body only; md/html interpretation left to the server).
7. pull converts HTML→Markdown via `markdownify` and saves `.md` (lossy round-trip).

### 1.3 The user's writing style and requirements

- Figures are written in HTML (`<figure><img src="..." width="80%"><figcaption>…</figcaption></figure>`). They
  dislike the default Markdown image rendering and always use this form.
- LaTeX math (`$...$` / `$$...$$`).
- Prefers explicit control. Automatic conflict handling and bidirectional "clever" sync should be minimal.
- Push-centric (local is the source of truth). But **they do sometimes edit the body in the eLabFTW Web UI**.
- Do not want secrets (API keys) sitting in the project (to avoid accidental GitHub commits).

### 1.4 Why build our own (the core mismatch)

`elab-doc-sync` is designed as "an add-on that auto-uploads by special-casing the single `![]()` notation," so the
user's HTML `<figure>` style falls outside its automation. What the user actually wants is the opposite
generalization:

> **Notation-independent: upload every real local path the body references, and replace that path with the real
> eLabFTW URL.**

With this one engine, `<img src="fig.png">`, `[data](x.csv)`, and `![a](f.png)` are all "uploaded + real-URL'd"
under the same rule, dissolving the asymmetry, broken links, and lack of figure support at once.

### 1.5 Approach: build `elab` fresh, treat `elab-doc-sync` as a reference

`elab` is a clean new design (not a fork inheriting existing code). That said, `elab-doc-sync` is a well-made
**reference implementation**: for its API client, change detection, reverse transclusion (`_download_images`), and
similar, **consult it and pull in only what is needed** when a missing feature becomes wanted.

---

## 2. Scope and design principles

### 2.1 Design principles

- **Single engine**: "referenced local path → upload + real-URL replacement" (notation- and type-independent).
- **Markdown-native**: send raw Markdown as the body. Do not mangle it with conversion.
- **Explicit and simple**: no hidden automation. Local is authoritative; push is a full-body overwrite (with
  conflict detection).
- **Secret isolation**: credentials live only under the home dir + the OS vault. No secrets in the project.
- **Stay thin**: add wanted features by consulting `elab-doc-sync` and taking only what's needed (§1.5).

### 2.2 In scope (MVP)

- **push** a single document (e.g. `report.md`) as one entity.
- Upload every real local file the body references, **regardless of notation or type**, and replace the reference
  with the real URL.
- Send raw Markdown (`content_type=2`). `<figure>` and math render as-is on the eLabFTW side.
- **pull** (with reverse transclusion).
- **Conflict detection + git-delegated merge**.
- **`status` and `diff`** (read-only, side-effect-free). The git-like core UX.
- YAML front matter for `elab_id` / `title` / `tags` / `category` (+ optional `profile`).
- Multiple profiles (multiple teams/instances) and layered configuration.
- Auth check (`whoami`), `.elabignore` exclusion, and an upload listing shown at push time.

### 2.3 Out of scope (not for now)

- An in-tool merge engine (merge is **delegated to git**).
- Automatic bidirectional sync / automatic conflict resolution.
- Folder conventions like `attachments_dir` (unnecessary under the reference-based approach).
- GUI / TUI.

---

## 3. Settled decisions (fixed during the design sessions)

1. **Name / command**: `elab` (a user-facing sync CLI; not Markdown-only; formerly `elabmd`). Like `gh` (tool) vs
   GitHub (service), the tool name `elab` and the target service `eLabFTW` are distinct concepts.
2. **Core**: upload referenced real local paths regardless of notation/type + replace with real URLs. Attachment =
   local path; link to another note = eLab URL.
3. **Body format**: send raw Markdown (`content_type:2`). No conversion, no math protection. Abort if
   `content_type` is not 2 after push (an old buggy version) — see [ELABFTW-API.md](ELABFTW-API.md) § content_type.
4. **Figures / math**: `<figure>` stays raw HTML; `$...$`/`$$...$$` stay as-is. eLab renders them.
5. **pull**: with reverse transclusion. If clean, update `report.md` in place.
6. **Conflict / merge**: store base in `~/.config/elab/state/` (key = namespaced `base_url + entity kind +
   elab_id`) in **two forms, remote-base / local-base**, and **delegate merge to git**. A pull with no base
   degrades to 2-way; a push to an existing entity with no base aborts (guide to pull or `--force`). eLab history is
   the recovery safety net. Revisions are retrievable via the API but cannot be tied to "which one was my last
   push" (ambiguous ancestor), so they are not used to auto-restore base; the local base is primary.
7. **Blast-radius control**: `.elabignore` + an upload listing at push time + confirmation for large new uploads.
8. **Auth**: keyring (OS vault) + base_url in TOML + env override. No secrets in the project.
9. **Profiles / config**: multi-team support. A settings.json-style 3-layer configuration.
10. **Metadata placement**: front matter = elab_id + title/tags/category (+ optional profile). base = state
    (namespaced key, two forms remote/local + team ID). Attachment dedup prefers the remote hash, falling back to
    size (hash is compared on the fly, never stored); no automatic attachment deletion during push.
11. **File name**: not fixed (default `report.md`, any name allowed; sidecar `<name>.remote.md`).
12. **Default entity kind**: `experiments` (overridable in front matter).
13. **`status` / `diff` are MVP**: read-only, side-effect-free (`status` = preflight display, `diff` = source-form
    comparison). `log`/`show` (revision browsing) are out of MVP.

> The **measured facts** that justify keeping a two-form base locally, and why server normalization bears directly
> on conflict detection, live in [ELABFTW-API.md](ELABFTW-API.md). The **behavioral contract itself** (span-based
> replacement, same-form comparison, never deleting attachments, etc.) is owned by the **tests**. This document
> holds only the "why."

---

## Appendix. `elab-doc-sync` reference

- Repository: <https://github.com/Kosaku-Noba/elab-doc-sync> (0.4.2 at time of trial).
- Install location: `~/.local/share/uv/tools/elab-doc-sync/lib/python3.14/site-packages/elab_doc_sync/`.
- Reference implementation: `sync.py` (`IMAGE_RE`, `_rewrite_images`, `_download_images` = reverse transclusion,
  `_md_to_html`, `_sync_attachments`); `client.py` (`ELabFTWClient`, `upload_file`, `list_uploads`).
