# Workflow

A document maps to one entity, and the local file is the source of truth. push overwrites the remote body, after
checking that nobody changed it since your last sync.

## The loop

```sh
elport status        # what would sync, and the entity's mode; sends nothing
elport diff          # local against remote, in source form; sends nothing
elport diff --base   # what you changed since the last sync
elport push -n       # the uploads, the path-to-URL plan, and why push would stop; sends nothing
elport push
```

On a directory, these commands act on every document under it and print one line each. push, pull, fetch,
status, and diff exit with 1 if any document failed.

`local: clean` means the body is unchanged since the last sync. Attachments and front matter may still differ.

## When the remote changed

push compares the remote body with the base it stored at the last sync. If someone edited the entity in the Web
UI, push stops with `remote changed` and writes two files beside the document: `<name>.base.md`, the last synced
version, and `<name>.remote.md`, the current remote. Then:

```sh
elport merge         # 3-way merge into the document; needs git
# resolve any <<<<<<< and >>>>>>> markers; push refuses a body that still has them
elport push
```

pull writes the same two files when the document has edits you have not pushed.

A machine that never synced the entity has no base. Run `elport pull` before the first push there.

`--force` skips the comparison and discards the remote edit. elport keeps no history of its own, so to keep one,
`git init` your notes folder.

A locked entity is refused, even with `--force`. Unlock it in the Web UI.

## pull and fetch

- pull brings down the body and metadata, and downloads the files the body shows. A path your push wrote comes
  back as written and is not downloaded again. An attachment added in the Web UI comes down by its base name into
  the document directory.
- fetch downloads every attachment, shown in the body or not, and never touches the body or the base. A local file
  that differs stays, and the remote copy lands beside it as `<name>.remote`.
- Neither writes a hidden (dot-prefixed) attachment.

## Reading without a working copy

`elport view <id>` prints a body in readable form. `elport view <id> -o DIR` also downloads the files the body
shows, into a new or empty directory. That copy has no front matter and no base, so push never picks it up. To
edit an entity, run `elport clone <id>`.

`elport clone --all DIR` backs up every entity, and a re-run adds new ones. An HTML entity is saved the way
`view -o` saves it.
