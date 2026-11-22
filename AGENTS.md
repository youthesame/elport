# elport: project guide for AI agents

`elport` is a **git-like sync CLI for eLabFTW**. It pushes and pulls local Markdown documents, and the files they
reference, to and from eLabFTW entities. **Local is the source of truth.**

## Documentation map (what is authoritative for what)

- **Behavior = the tests** (`tests/`). To change behavior, **update the tests first**, then implement. Do not
  restate the contract in prose: a restatement drifts.
- **Terms = [CONTEXT.md](CONTEXT.md)**. Use its words, and not the ones it lists under _Avoid_, in code, messages,
  tests, and docs.
- **Why it is built this way = [docs/DESIGN.md](docs/DESIGN.md)** (frozen). Later decisions live in
  **[docs/adr/](docs/adr/)** and take precedence; DESIGN is not edited to match, it only gains a pointer.
- **Measured eLabFTW API facts = [docs/ELABFTW-API.md](docs/ELABFTW-API.md)**. Read it before touching requests,
  URLs, or body and metadata handling: the API has traps the mocked tests cannot show. Update it only when the
  demo's version changes.
- **User-facing usage = [README.md](README.md)** and the help topics in **[src/elport/help/](src/elport/help/)**
  (`elport help <topic>` prints them; README links to them instead of repeating them). Write each entry briefly
  and in step with the text around it: what a user needs to use the feature, not its edge cases or rationale.
- **Driving elport from an agent = `elport help agents`** ([src/elport/help/agents.md](src/elport/help/agents.md)).
  [skills/elport/SKILL.md](skills/elport/SKILL.md) only says that elport exists and points there.

Design principles (DESIGN §2.1): single engine / markdown-native / explicit-and-simple / secret isolation. Keep
added features **thin** (YAGNI); prefer read-only, single-endpoint features.

## Invariants you must not break

One rule per bullet. The tests hold the detail and the ADRs the reasons: when a decision refines a rule, link its
ADR here and leave the detail there.

- **Local is authoritative.** A push that sends the body overwrites all of it, after conflict detection. No silent
  two-way sync.
- push rewrites references on the **outgoing copy** and never changes the local document.
- **The base has two forms**: remote-base (the stored body returned by the push `PATCH`, not the bytes sent;
  [ADR-0002](docs/adr/0002-remote-base-from-the-patch-response.md)) and local-base. **Compare same-form only**
  (remote↔remote-base, local↔local-base): the server normalizes every save without converging, so a single form
  gives false conflicts or a document that stays dirty.
- **Nothing out of sync goes into the body** (comments, steps, revision bodies). Print it to the terminal.
- **Only files the user wrote a path to are uploaded**, wherever they are on disk. Hidden files and directories
  (any dot-prefixed path component, as written or as resolved) never are
  ([ADR-0007](docs/adr/0007-references-may-leave-the-document-directory.md)).
- **Remote content (a body, a URL, an attachment name) cannot authorize a local upload.** A remote-linked path is
  uploaded only when listed under `attachments:` ([ADR-0006](docs/adr/0006-front-matter-attachments.md), ADR-0007).
- **Never parse inside code fences, inline code, or HTML comments.**
- **pull never writes outside the document directory.**
- A link to another document is sent as its entity URL and **never uploaded**; pull turns back only what push
  recorded ([ADR-0003](docs/adr/0003-folder-sync-and-document-links.md)).
- An attachment is reused only when **its server-returned sha256 matches** the local file
  ([ADR-0014](docs/adr/0014-only-a-matching-sha256-reuses-an-upload.md)). **Never delete attachments during push.**
- **push refuses a locked entity, even with `--force`**
  ([ADR-0004](docs/adr/0004-refuse-pushing-to-locked-entities.md)).
- **clone, pull, and push refuse an HTML entity whose body is not empty**
  ([ADR-0010](docs/adr/0010-html-entities-are-read-only.md)).
- **Keep API keys out of the project tree and out of logs.** Stored keys live in the OS keyring or home-dir config.

## Testing and verification

- Tests run **without network** (the API layer is mocked). CI runs `uv run pytest`, `uv run ruff check`,
  `uv run ruff format --check`, and `uv run ty check src`.
- Property tests live in `tests/test_properties.py` (hypothesis) and run in CI on the default backend.
  `HYPOTHESIS_PROFILE=crosshair uv run pytest tests/test_properties.py -m symbolic` explores the fast pure-Python
  subset symbolically; without `-m symbolic` that takes minutes. CrossHair runs are local only.
- To confirm live API behavior, use **only the public demo <https://demo.elabftw.net>** (public, periodically
  reset, disposable). Never use any other instance. Record the demo version a fact was measured on;
  version-dependent behavior is confirmed on the demo's current version only.
