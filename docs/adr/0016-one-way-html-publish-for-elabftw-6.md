---
status: proposed
---

# One-way HTML publish for eLabFTW 6.x

eLabFTW 5.x is end-of-life and 6.x is the only supported line. As measured on demo 6.0.9
([ELABFTW-API.md](../ELABFTW-API.md) § content_type), Markdown entities (`content_type: 2`) strip
raw HTML (`<figure>`, `<img width>`, `<sub>`/`<sup>`, `<br>` in tables, `<details>`), offer no
image-sizing syntax, and corrupt inline TeX math (`$a*b*c$`, `$\{x\}$`), whereas HTML entities
(`content_type: 1`) preserve them through `Filter::body()` and converge byte-for-byte on re-PATCH.
elport therefore adds an opt-in one-way HTML publish mode (`content_type: 1`) with no
HTML-to-Markdown reverse conversion, while keeping existing Markdown sync (`content_type: 2`) for
5.x and pure-Markdown workflows.

- **Opt-in publish and coexistence (refines [ADR-0010](0010-html-entities-are-read-only.md)).**
  New entities may opt into HTML publish (`content_type: 1`). Existing Markdown entities stay on
  `content_type: 2` (`clone`, `pull`, `merge`, `push` unchanged) until migrated explicitly per
  entity against a matching remote-base, re-checking body, `content_type`, and lock before the
  PATCH (which narrows, but does not atomically eliminate, the race window). HTML publish
  registration is recorded in local state after verifying `content_type == 1` and the stored body
  from the PATCH response; unregistered non-empty HTML entities (such as a colleague's Web UI
  entity) and locked entities remain refused even with `--force`. `clone` and `pull` continue to
  refuse HTML entities; `view` and snapshots keep working as before. When implemented, update
  `CONTEXT.md` (HTML entity definition) and `AGENTS.md` to match.
- **Reference planning stays on the Markdown source.** `plan()`, `.elportignore`, listed
  attachments (`attachments:`), document links, dotfile blocking, and remote-linked path
  protection ([ADR-0006](0006-front-matter-attachments.md),
  [ADR-0007](0007-references-may-leave-the-document-directory.md)) run on the Markdown document
  first to produce the outgoing Markdown copy. Only that approved outgoing copy is rendered to
  HTML, preserving raw `<figure>` and protecting inline and display TeX math (`{width=...}`
  shorthand is deferred). Local dirty checks hash the Markdown source rather than rendered HTML so
  renderer updates never dirty unchanged documents. Pushing never requires a clean Git tree.
- **Base stays in `~/.config`, with optional portable remote-base handoff.** Everyday base state
  remains under `~/.config/elport/state/`, so single-machine use needs no external sync tool and
  Git branch switches, rollbacks, dirty pushes, or cloud-folder lag do not invent false conflicts.
  For multi-machine editing (via Git or a synced folder), elport may later carry an explicit
  handoff (`instance URL`, `entity type`, `entity ID`, `content_type`, `body_sha256`, and
  `meta_base` when syncing metadata) in a dedicated sidecar outside front matter, keeping body
  digest checks separate from HTML publish registration ([ADR-0002](0002-remote-base-from-the-patch-response.md)).
- **Canonical HTML diff is for review, never for auto-adopting a base.** Because TinyMCE
  reformats whitespace across the body on save ([ELABFTW-API.md](../ELABFTW-API.md) § content_type),
  a canonical HTML normalizer (normalizing whitespace outside `<pre>` and keeping semantic
  attributes `img[src|width|height|style]`, `a[href]`, `pre[class]`) displays Web UI edits cleanly
  against the stored remote-base. Re-pushing after porting Web UI edits confirms against the
  reviewed remote body hash rather than blind `--force`. Block-level surgical pull against local
  local-base and remote-base remains an optional later layer without changing the server format.
- **Protocol verification and delivery order.** Before implementing, model the multi-step
  publish/handoff/conflict/lock/registration state machine in Quint (for automated counterexample
  search and JSON trace vectors) or Lean 4 (for unbounded invariant proofs) to verify protocol
  safety and export language-independent test vectors, closing the model once the protocol
  settles. Implement HTML publish in Go after the behavior-preserving Go port
  ([ADR-0017](0017-go-migration-and-distribution.md)), initially as pre-release / nightly.

## Acceptance criteria

Before broadening HTML publish beyond opt-in:

1. Existing Markdown entities keep their exact `content_type: 2` behavior; registered HTML
   entities re-push cleanly and never plan new local attachments from renderer-generated output.
2. A second machine with local edits can push when its carried remote-base matches the server's
   current body, and stops if the Web UI changed an image width, link target, or text.
3. Unregistered non-empty HTML entities and locked entities cannot be overwritten even with
   `--force`.

## Considered options

- **Narrow bidirectional HTML-to-Markdown conversion.** Introduces `<pre>`-to-reference upload
  safety holes, fragile block alignment, dual-path Git/elport merge conflicts, and 2x syntax cost.
- **Store the Markdown source as an active or archived (`state: 2`) attachment.** Active
  attachments add visible double-management; archived single-copy management adds +3 API calls per
  push, changelog churn, soft-deleted (`state: 3`) rows, and timestamp `immutable` conflicts.
- **Stay on `content_type: 2` Markdown and resize image pixels.** Blurs HiDPI plots, duplicates
  attachments, and leaves captions, `<sub>`/`<sup>`, table `<br>`, and inline TeX math broken.
- **In-repo base or `canon_html` auto-adoption.** In-repo base clashes with Git branches and dirty
  pushes; `canon_html` auto-adoption fails on path vs URL mismatch and conflates display with
  overwrite authority.
- **Combine HTML publish with the Go port in one step.** Removes the frozen Python baseline as a
  parity oracle ([ADR-0017](0017-go-migration-and-distribution.md)).
