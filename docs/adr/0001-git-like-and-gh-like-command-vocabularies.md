# Two command vocabularies: git-like for the sync loop, gh-like for remote browsing

elport's commands draw on two distinct vocabularies, and this is deliberate rather than an
inconsistency to be cleaned up:

- **git-like** (`push` / `pull` / `status` / `diff` / `merge`) for the local sync loop — commands
  that act on a local document and its base.
- **gh-like** (`list` / `view`) for remote browsing — read-only commands that let a user find and
  read entities they have no local document for.

The two are not in tension. elport is git-*like* but is not git (DESIGN §"Not git-backed"), and `gh`
itself coexists with `git` for exactly this reason: `git log` reads local history, `gh issue list`
reads the remote. elport sits in the same position, so it inherits the same split. The audience is
also the same one — a user who reaches for a git-like sync CLI is overwhelmingly likely to already
have `gh` reflexes.

The practical consequence is that **DESIGN §3-13 and §"Deferred" no longer reserve `log` / `show`**.
Those lines were an MVP scope cut ("revision browsing is out of MVP"), not a name reservation; no
implementation depends on them. They are lifted here for two reasons. First, the git analogy that
produced those names is weak: `git show` displays a commit that is a known ancestor of your work,
whereas eLabFTW revisions provably cannot be tied to "which one was my last push" (DESIGN §3-6), so
whether revision browsing is worth building at all is still undecided — and an undecided feature
should not hold a name. Second, revision browsing reads the API, so under the split above it is a
gh-like command; borrowing git's `log`/`show` would imply the local ancestry that eLabFTW revisions
cannot supply, so an explicit `elport revisions` describes it better than a git-borrowed name.

## Consequences

- `list` and `view` are the sanctioned names for remote browsing, and `show` is free.
- New commands should be placed in one vocabulary consciously, by **what they operate on** rather
  than by whether they write locally. A command that operates on a local document and its base is
  git-like and is bound by the sync invariants (AGENTS.md); a command that reads a remote entity the
  user has no local document for is gh-like and stays read-only and side-effect-free.
- Auth and configuration commands (`login`, `logout`, `profile`, `whoami`) sit in neither set. They
  operate on credentials and config, not on an entity or a document, so the split does not classify
  them and the sync invariants do not constrain them.
- This ADR, not DESIGN, is the live record for command-vocabulary decisions. DESIGN remains frozen
  as the record of what was settled during the 2026-08-06..08 sessions.
