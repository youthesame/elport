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

If someone edited the entity in the Web UI since your last sync, push stops and writes two files beside the
document: `<name>.base.md`, the last synced version, and `<name>.remote.md`, the current remote. pull does the same
when the document has edits you have not pushed. Then:

```sh
elport merge         # 3-way merge into the document; needs git
# resolve any <<<<<<< and >>>>>>> markers; push refuses a body that still has them
elport push
```

elport keeps no history of its own. To keep one, `git init` your notes folder.

## pull and fetch

- pull brings down the body and metadata, and downloads the files the body shows. An attachment added in the Web
  UI comes down by its base name into the document directory.
- fetch downloads every attachment, shown in the body or not, and never touches the body or the base. A local file
  that differs stays, and the remote copy lands beside it as `<name>.remote`.
- Neither writes a hidden (dot-prefixed) attachment.
