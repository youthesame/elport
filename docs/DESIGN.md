# elport: Design Record (why it is built this way)

- **Nature**: a frozen document. It captures the **rationale and settled decisions** behind elport. Rarely updated.
- **What it is not**: not a behavioral spec (the **tests** are authoritative for behavior); not a usage guide
  (that is the [README](../README.md)); not a record of measured eLabFTW API facts (that is
  [ELABFTW-API.md](ELABFTW-API.md)).
- **Origin**: design sessions of 2026-08-06..08 plus external review.

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

### 1.5 Approach: build `elport` fresh, treat `elab-doc-sync` as a reference

`elport` is a clean new design (not a fork inheriting existing code). That said, `elab-doc-sync` is a well-made
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
- YAML front matter for `id` / `title` / `tags` / `category` (+ optional `profile`).
- Multiple profiles (multiple teams/instances) and layered configuration.
- Auth check (`whoami`), `.elportignore` exclusion, and an upload listing shown at push time.

### 2.3 Out of scope (not for now)

- An in-tool merge engine (merge is **delegated to git**).
- Automatic bidirectional sync / automatic conflict resolution.
- Folder conventions like `attachments_dir` (unnecessary under the reference-based approach).
- GUI / TUI.
- **Cross-entity attachment resolution.** A `download.php` URL that points at another entity's upload stays an
  opaque URL, and the tool never localizes it. Its query carries no entity or upload id, and `download.php` needs cookie
  auth, so the tool can neither fetch it nor turn it back into a local filename. Localizing it would re-upload the
  file as this entity's own attachment and duplicate it. The tool names such references rather than letting them
  pass silently.

---

## 3. Settled decisions (fixed during the design sessions)

1. **Name / command**: `elport` (a user-facing sync CLI; not Markdown-only; formerly `elabmd`). Like `gh` (tool) vs
   GitHub (service), the tool name `elport` and the target service `eLabFTW` are distinct concepts.
2. **Core**: upload referenced real local paths regardless of notation/type + replace with real URLs. Attachment =
   local path; link to another note = eLabFTW URL.
3. **Body format**: send raw Markdown (`content_type:2`). No conversion, no math protection. Abort if
   `content_type` is not 2 after push (an old buggy version). See [ELABFTW-API.md](ELABFTW-API.md) § content_type.
4. **Figures / math**: `<figure>` stays raw HTML; `$...$`/`$$...$$` stay as-is. eLabFTW renders them.
5. **pull**: with reverse transclusion. If clean, update `report.md` in place.
6. **Conflict / merge**: store base in `~/.config/elport/state/` (key = namespaced `base_url + entity kind +
   id`) in **two forms, remote-base / local-base**, and **delegate merge to git**. A pull with no base
   degrades to 2-way; a push to an existing entity with no base aborts (guide to pull or `--force`). eLabFTW history is
   the recovery safety net. Revisions are retrievable via the API but cannot be tied to "which one was my last
   push" (ambiguous ancestor), so they are not used to auto-restore base; the local base is primary.
7. **Blast-radius control**: `.elportignore` + an upload listing at push time + confirmation for large new uploads.
8. **Auth**: keyring (OS vault) + base_url in TOML + env override. No secrets in the project.
9. **Profiles / config**: multi-team support. A settings.json-style 3-layer configuration.
10. **Metadata placement**: front matter = id + title/tags/category (+ optional profile). base = state
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

## 4. Settled decisions, 2026-08-11 (git relationship, merge, permissions, comments)

A grilling session sharpened the git story and set the next feature batch. These **append** to §3; they refine §3.6
rather than revising the earlier decisions.

**Not git-backed; git is used for one thing only.** elport is git-*like* in UX (push/pull/status/diff) but is **not**
git-*backed*: no repository is required, the base lives in `~/.config/elport/state/` (never in the user's tree), and
elport never commits or touches the user's git history. "Local is the source of truth" does not entail git: forcing a
git workflow would constrain how researchers organize notes. The **only** place elport reaches for git is conflict
merge. (Want per-edit local history? That is the user's own `git init`, which elport neither requires nor manages.)

**`elport merge` wraps `git merge-file`.** The stored two-form base is a genuine common ancestor, so conflict
resolution is a real 3-way merge: `git merge-file <local> <base> <remote>`, with the remote sidecar
reverse-transcluded and the base sidecar taken from the local base, written as `<name>.remote.md` /
`<name>.base.md`. Refinement of §3.6: rather than only *printing* that command,
`elport merge <note>` *runs* it (writing the result into the local file, with markers on overlap). push/pull stay
**non-mutating on conflict**: they emit the sidecars and stop; `elport merge` is the explicit, opt-in step that
rewrites local, and it **never auto-pushes**. git stays **optional** (fallback: if `git` is absent, `elport merge`
points at the sidecars for a manual merge, and the sidecars are always written regardless). git is a system binary, so
it is a documented runtime requirement for merge only, never a `pyproject.toml` / PyPI dependency.

**The merge ancestor is the local base verbatim, never the reverse-transcluded remote base.** `git merge-file`
takes one ancestor, but the stored base is two-form (§3.6). `local-base` is in local form (filenames); `remote-base`
is in server form (`download.php` URLs). The other two merge inputs are already local form: the working file, and the
remote body reverse-transcluded to filenames. So the ancestor is `local-base`, which makes `local` vs `base` a
same-form diff that carries only the user's edits. Reversing `remote-base` for the ancestor would bring back the two
false conflicts the two-form base exists to prevent. First, server body normalization, which does not converge (see
the normalization section of ELABFTW-API). Second, an attachment deleted in the Web UI, whose `download.php` URL no
longer resolves to a filename and so diffs against the ancestor's filename. `remote-base` is still reverse-transcluded,
but only to find which base attachments still exist and can be placed as `<name>.base`. One ancestor cannot be
same-form with both sides at once, so the merge keeps the local diff clean and lets the remote diff carry the noise.
Local is authoritative, so the user's edits stay intact. This never invents a conflict from normalization alone,
because `_raise_conflict` runs only when the remote actually moved (`remote_body != remote_base`).

**push refuses unresolved conflict markers.** If the outgoing body carries `<<<<<<<` / `=======` / `>>>>>>>` marker
lines, push aborts (bypass with `--force`). This closes the one hole this stance would otherwise leave: a
half-resolved merge silently pushed to the server.

**Permissions sync.** Front matter gains independent `read:` / `write:`, mapping keywords to eLabFTW base
levels: `owner`=10, `owner+admin`=20, `team`=30, `account`=40, `public`=50 (`canread_base` / `canwrite_base`).
Applied **only when declared** (absent ⇒ untouched, so a routine push never reverts an intentional Web-UI change,
matching the add-only-tags safety stance). **Base-level only**: elport never sends the individual allow-list JSON, so
Web-UI-set individual grants survive (measured: PATCHing only `*_base` preserves the `canread`/`canwrite` JSON).
Because base and the individual list are OR'd, two warnings guard the surprises. Widening to `account`/`public`
(leaves the team) prompts confirmation; narrowing to `owner`/`owner+admin` while individual grants persist warns
that effective access is still wider than the keyword implies. `--yes` skips; a non-interactive run without it aborts
rather than act blindly.

**Comments.** Read via `elport comments <note>` to the terminal only. Comments are out-of-sync data and
must not enter the body or a written sidecar. Post via `elport comment <note> "..."`. No edit/delete (rare from a CLI;
the Web UI handles it).

**Deferred.** Revision browsing (`elport log` / `elport show`, read-only), task steps, and reciprocal links are out of
this batch (their measured API shape is in [ELABFTW-API.md](ELABFTW-API.md)).

---

## Appendix. `elab-doc-sync` reference

- Repository: <https://github.com/Kosaku-Noba/elab-doc-sync> (0.4.2 at time of trial).
- Install location: `~/.local/share/uv/tools/elab-doc-sync/lib/python3.14/site-packages/elab_doc_sync/`.
- Reference implementation: `sync.py` (`IMAGE_RE`, `_rewrite_images`, `_download_images` = reverse transclusion,
  `_md_to_html`, `_sync_attachments`); `client.py` (`ELabFTWClient`, `upload_file`, `list_uploads`).
