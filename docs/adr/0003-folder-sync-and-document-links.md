# Folder sync: many documents, links between them, and `clone --all` as the mirror

> Superseded in part by [ADR-0007](0007-references-may-leave-the-document-directory.md):
> any reference may now leave the document directory.

`push`, `pull`, `status`, `fetch`, and `diff` take any number of paths, and a directory expands to
every **elport document** under it. A link from one document to another becomes the target's entity
URL. A backup of the whole account is `clone --all <root>` followed by `pull <root>`. That makes a
tree of notes behave like a repository instead of a pile of separately synced files.

## Decisions

- **Only elport documents are picked up from a directory.** An elport document is Markdown whose
  front matter holds only elport keys and includes both `id` and `profile`. elport writes those two
  keys itself, so another tool's front matter (Hugo, Jekyll) or a bare `title:` never matches by
  accident. A file that does not match is skipped and only counted. A file named explicitly keeps
  the single-document behaviour. Sidecars (`*.remote.md`, `*.base.md`) and hidden paths are never
  picked up.
- **A link to an elport document is rewritten, never uploaded.** If the target has no `id`, the push
  stops. A link to plain Markdown is still an ordinary attachment. This is the one place where `..`
  may leave the document directory: elport reads only the target's front matter, and only the
  target's URL leaves the machine. Hidden paths stay excluded.
- **eLabFTW links are add-only and tracked in state.** A same-server target is registered as an
  eLabFTW link once. The registered set lives in state, so a link removed in the Web UI stays
  removed, and nothing is ever deleted. A target on another server gets the URL only.
- **pull turns a link back into its path only from the push record.** push records which sent URL
  came from which written destination, and pull reverses exactly those. A URL pasted in the Web UI
  stays a URL. A pulled body can therefore never name a local path that push did not write, which
  keeps the exfiltration invariant intact without defining a project root.
- **State records the document's path.** A base is keyed by server, entity, and id, not by file. If
  the recorded file still exists and holds the same entity, the second copy is refused. If it is
  gone, the move is followed silently.
- **A batch reports one line per document and keeps going.** It exits 1 if any document failed.
  `--force` is refused for more than one document, because it discards Web-side edits. Prompts
  cannot be answered in a batch, so a confirmation fails that document unless `-y` is given.
- **The mirror is `clone --all`, not a new command.** It clones every entity that is not already in
  the tree into `<root>/<entity>/<id>-<title>/report.md`, so it is safe to re-run. The title keeps
  Unicode (NFC), replaces filesystem-unsafe characters with `_`, and is cut at 50 characters. The
  directory is never renamed when the title changes. The default scope is self, `--scope team|all`
  widens it, and both entity types are included unless `--entity` is given. An entity deleted on the
  server is reported, never deleted locally.

## Considered and rejected

- Every `*.md` under a directory is a document: READMEs and scratch notes would become entities.
- A separate `mirror` command: it would duplicate `clone` plus `pull`.
- Reverse-mapping by scanning a project root: it needs a root definition, and it lets a remote body
  pick local paths.
- Automatic provenance (git commit in `extra_fields`): it breaks for non-git work, code kept
  elsewhere, and multi-repo projects.
