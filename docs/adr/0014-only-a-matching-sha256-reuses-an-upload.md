# Only a matching sha256 reuses an upload

push reused an upload without a sha256 when its name and size matched the local file. A file edited in
place at the same size (a fixed-width CSV with one value changed, say) then matched its old upload: push
linked that, the remote kept serving the old content, and the edit was never uploaded. Nothing reported
it, and status showed the document up to date.

- **An upload is reused only when its sha256 matches the local file.** One the server lists without a
  sha256 is never reused; the file is uploaded again.
- On a server that lists no hashes, every push that references files uploads them again. Duplicate
  attachments are visible and harmless; a link to the wrong content is neither.

Every eLabFTW we measured lists a sha256 (ELABFTW-API, "File uploads"), so the cost falls only on
instances old enough not to.

## Considered options

- **Keep name + size as a fallback.** It saves uploads only on hashless instances, and it is exactly
  where the wrong link comes from.
- **Hash the remote copy by downloading it.** Correct, but every push would download every referenced
  attachment to compare.
