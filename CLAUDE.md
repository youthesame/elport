# elab — project guide for AI agents

`elab` is a **git-like sync CLI for eLabFTW**. It push/pulls locally-written lab notes (Markdown/HTML + referenced
files) to/from eLabFTW entities. **Local is the source of truth.**

## Documentation map (what is authoritative for what)

**Authority differs by role:**

- **Behavioral contract (authoritative) = the tests** (`tests/`). The core engine (reference scan/replace, path
  safety, reverse transclusion) and the sync semantics (push/pull/status/diff, conflict detection, the two-form
  base) are owned solely by the tests. **To change behavior, update the tests first**, then implement. Do not
  create prose restatements of the contract (they become a drift source).
- **Why it is built this way = [docs/DESIGN.md](docs/DESIGN.md)** (frozen: background, scope, settled decisions).
- **Measured eLabFTW API facts = [docs/ELABFTW-API.md](docs/ELABFTW-API.md)** (observation log of the external
  world; update only when the target instance version changes).
- **User-facing usage = [README.md](README.md)** (commands, front matter, config).
- **Driving elab from an agent = [skills/elab/SKILL.md](skills/elab/SKILL.md)** (for the downstream operating
  context).

Design principles (DESIGN §2.1): single engine / markdown-native / explicit-and-simple / secret isolation. Keep
added features **thin** (YAGNI); prefer read-only, single-endpoint features.

## Invariants you must not break (quick reference — detailed authority is the tests, rationale is DESIGN)

- **Local is authoritative.** push is a full-body overwrite with conflict detection. Do not silently add two-way
  auto-sync.
- push transclusion runs on the **outgoing copy** and **never rewrites the local original**.
- **base is two-form**: remote-base (the body re-`GET`'d after a successful push, not the sent bytes) and local-base
  (the local original at push/pull time). **Always compare same-form** (remote↔remote-base, local↔local-base). With
  only one form, server normalization (non-convergent) yields a false conflict / permanent-dirty every time — see
  the "Body normalization" section of ELABFTW-API.
- **Do not embed out-of-sync data (comments, steps, revision bodies) in the body.** Use a sidecar or terminal
  output (do not pollute the source).
- Upload targets are **only files under the document directory.** Exclude schemed URLs, absolute paths, and `..`
  escapes; **never parse inside code fences, inline code, or HTML comments.**
- Attachment identity uses the **server-returned hash first, else basename + size** (same-name entries can coexist,
  so check against all of them). **Never auto-delete attachments during push.**
- **Keep secrets (API keys) out of the project tree.** OS keyring + home-dir config only. **Never log the key.**

## Non-obvious eLabFTW API traps (highlights — details in ELABFTW-API.md)

- **`content_type:2` (markdown) is settable via PATCH and respected by recent eLabFTW** (the create-time body bug
  #6416 is fixed; very old instances may ignore it). **Verify `content_type == 2` after push** and abort otherwise.
- **`download.php?...` URLs use browser Cookie-session auth** and **do not work with the API key.** Embed them in
  the body for humans, but the tool's own download (pull) uses the API
  `GET /api/v2/{entity}/{id}/uploads/{id}?format=binary`.
- An upload's `long_name` **contains `/`**. When building download-URL queries, **percent-encode is mandatory.**
- **The body is normalized on every save and re-sending the normalized form does not converge** (even in md mode).
  **HTML-unescape** URLs in the body before parsing them.
- **Revisions are readable** (`GET .../revisions[/{id}]`), but nothing ties a version to "my last push." Keep them
  **read-only**; do not use them to auto-restore the 3-way merge ancestor base.

## Testing and verification

- The core engine (reference scan/replace, path safety, reverse transclusion) and the sync semantics are
  unit-tested **without network** (the API layer is mocked). These tests are the authority for behavior, so
  **changes go test-first.**
- When live API behavior needs confirming, use the **public demo <https://demo.elabftw.net>** (public, periodically
  reset, disposable). Confirm version-dependent behavior on the real target instance too (see the version-specific
  section of ELABFTW-API.md).
