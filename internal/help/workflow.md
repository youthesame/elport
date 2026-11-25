# Workflow

A document maps to one entity, and the local file is the source of truth. push overwrites the remote body, after
checking that nobody changed it since your last sync.

## The loop

```sh
elport status        # what would sync, and the entity's mode; sends nothing
elport diff          # local against remote, in source form; sends nothing
elport diff --base   # what you changed since the last sync
elport push -n       # the uploads, the path-to-URL plan, and why push would stop; sends nothing and skips prompts
elport push
```

On a directory, these commands act on every document under it and print one line each. push, pull, fetch,
status, and diff exit with 1 if any document failed.

`local: clean` means the body is unchanged since the last sync. Attachments and front matter may still differ.

## When the remote changed

If someone edited the entity in the Web UI since your last sync, push stops and writes two files beside the
document: `<name>.base.md`, the last synced version, and `<name>.remote.md`, the current remote. pull does the same
when the document has edits you have not pushed (`push -n` writes no sidecars, so run `elport pull` before `merge`
after a dry-run conflict). Then:

```sh
elport merge         # 3-way merge into the document; needs git
# resolve any <<<<<<< and >>>>>>> markers; push refuses a body that still has them
elport push
```

elport keeps no history of its own. To keep one, `git init` your documents folder.

## One-way HTML publish (`format: html`)

With `format: html`, `elport push` renders the approved outgoing Markdown to HTML (`content_type: 1`) and never
pulls HTML back into Markdown.

- **Migrate or revert**: Add `format: html` to an existing synced Markdown document (with no unpulled remote edits)
  and run `elport push` to switch it to HTML (`2→1`). Set `format: markdown` explicitly and `elport push` to revert a
  locally registered HTML document back to Markdown (`1→2`); omitting `format:` on an HTML entity is refused.
- **Web UI edits & second machines**: If someone edits the entity in the Web UI, `push` (even with `--force`) stops
  and prints a canonical HTML diff and the remote body's short SHA-256. Port any changes into your local Markdown
  document, verify with `elport diff`, and re-run `elport push --confirm-remote <hash>`. On a second machine without
  local state, the entity owner can adopt an existing HTML entity the same way (`elport diff`, then
  `elport push --confirm-remote <hash>`).
- **Metadata-only `pull`**: On a locally registered HTML document, `elport pull` updates only front matter
  (`title`, `category`, `status`, `read`, `write`, and the union of local and remote `tags`) without touching local
  body bytes or downloading attachments.

## pull and fetch

- pull brings down the body and metadata (or metadata only, with `tags` unioned, on a locally registered HTML
  document), and downloads the files the body shows. An attachment added in the Web UI comes down by its base name
  into the document directory.
- fetch downloads every attachment, shown in the body or not, and never touches the body or the base. A local file
  that differs stays, and the remote copy lands beside it as `<name>.remote`.
- Neither writes a hidden (dot-prefixed) attachment.
