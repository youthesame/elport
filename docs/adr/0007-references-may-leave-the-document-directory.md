# References may leave the document directory; only what the user wrote is uploaded

Until now a reference or an `attachments:` entry had to stay under the document directory, so data
kept elsewhere (a shared `data/` beside `notes/`, an instrument export folder) had to be copied next
to each note and managed twice. The boundary was a second wall. The threat it bounded is that a
remote body, pulled in, makes the next push upload a local file nobody here chose. That threat is
answered by provenance, not by where the file sits, so the boundary goes.

- **A reference may be `..` or absolute**, and so may an `attachments:` entry and a document link.
  Hidden components stay excluded: as written, and in the resolved file below the deepest
  directory it shares with the document directory. A document under a hidden directory still
  works; a symlink into `~/.ssh` does not. `~` is not expanded.
- **`.elportignore` applies only inside the document directory.** A file outside was named by
  its full path on purpose, and gitignore patterns have no meaning for a path that leaves their
  anchor.
- **A path that appears in a remote body is never uploaded.** push replaces every file it uploads
  with a URL, so a bare path in a remote body (the current one, the last synced one, an unmerged
  conflicting one) was written by someone else. push warns and sends the path as written. Listing
  the file under `attachments:` is the only way to upload it: front matter never comes from the
  server, so it is the user's own word. Respelling the path does not count, because server
  normalization and merges respell too. This replaces the confirmation prompt, which asked a
  question the user could not judge and which `-y` skipped.
- **push records each uploaded reference** (sent URL → destination as written) in the same state
  record as document links (ADR-0003). pull puts a recorded URL back verbatim and does not download
  it: the URL names the upload made from that local file, and local is authoritative. Only an
  unrecorded URL (added in the Web UI, or pulled on a machine without the record) still comes back
  as a basename in the document directory. pull therefore never writes outside the document
  directory, and `assets/fig.png` no longer comes back as `fig.png`.

This supersedes the "under the document directory" rule of ADR-0006 and the statement in
ADR-0003 that a document link is the only place `..` may leave it.

## Consequences

- An absolute path does not survive a `git clone` on another machine, and a machine without the
  record gets basenames from pull. Both are the cost of writing such a path.
- A path your own push sent unresolved (the file did not exist yet) is in the remote body, so once
  the file exists it counts as remote-linked. List it under `attachments:`.

## Considered Options

- **Keep the prompt and make `-y` not skip it for outside paths**: the user still cannot tell who
  wrote the path, and a prompt answered by reflex protects nothing.
- **Treat a local respelling as approval**: indistinguishable from normalization.
- **Bound uploads by a project root (git root, `.elport.toml`)**: a new concept to define and
  discover, and the instrument folder outside the repository would still need copying.
