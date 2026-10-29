# Front matter `attachments:` uploads files the body does not reference

> Superseded in part by [ADR-0007](0007-references-may-leave-the-document-directory.md):
> entries may now leave the document directory.

Until now the body was the manifest: push uploaded only files the body references, so a raw data
file had to be linked in the body to reach the entity. Issue #3 rejected uploading every local file
in the folder: it would push scratch files and `.env`, and without per-attachment state an
unreferenced file cannot tell "added here" from "deleted there". A list the user writes in front
matter avoids both problems. Only what is named goes up, and nothing is ever deleted, so no state is
needed.

- Each entry is a path to a file, checked by the same rules as a body reference: under the document
  directory (a `..` may not leave it), no hidden component, not excluded by `.elportignore`.
  Ignore rules match the file a symlink resolves to, not the spelling: they guard file contents,
  and a symlink cannot reach contents that its target's own path does not already expose.
- An entry that would not upload (missing, a directory, hidden, ignored, outside) **fails the push**.
  A body reference that cannot upload only warns, because a body legitimately holds paths that are
  not attachments. A list entry exists only to be uploaded, so skipping it would be a silent miss.
- Directories and globs are not accepted. Expanding them would have to skip hidden and ignored
  files silently, which contradicts the rule above.
- The body is not rewritten, and an attachment is reused by the usual hash/size match, so an
  unchanged list costs no upload and no `PATCH` (ADR-0005).
- Removing an entry does not delete the upload (push never deletes attachments). `pull` and `fetch`
  are unchanged: listed files stay out of `pull`, and `fetch` brings them down.

## Considered Options

- **Upload the whole folder** (or an `attachments_dir`, DESIGN §2.3): rejected for the reasons above.
- **An `elport attach <file>` command**: the front matter is already the index, so the command
  would only append a line to it.
