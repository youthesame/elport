# `view -o` writes a snapshot into a directory the caller names

> Superseded in part by [ADR-0013](0013-view-o-writes-every-attachment.md):
> `view -o` now writes every attachment, not only those the body shows.

An agent asked to look at an entity's figures had no way to do it with elport. `view` printed only the
body, whose images are `download.php` URLs that need a browser session, and `fetch` and `clone` need a
working copy, which the entity already had elsewhere. To look at a file, the agent needs it on disk after
the command returns, so `view <id> -o DIR` writes a snapshot there. This amends
[ADR-0001](0001-git-like-and-gh-like-command-vocabularies.md), which calls gh-like commands
side-effect-free: `view -o` writes local files, but never a document or a base.

- **The caller names the directory, and it must be new or empty.** elport keeps no cache and has no
  `clean` command. The OS does not reliably clear temp directories (Ubuntu clears `/tmp` only at boot,
  Windows not at all), and a fixed directory under `/tmp` is shared with other users. An empty target
  also means a snapshot never has to handle a name collision with an existing file.
- **Only what the body shows comes down.** A snapshot exists to be read, so attachments the body does
  not show, often large raw data, stay on the server.
- **A snapshot is never a working copy.** It has no front matter and no base, so any number can
  coexist with the entity's one working copy, and push and pull never pick one up. elport refuses a
  remote body that starts with elport front matter, and a shown attachment with the body's file name.

## Considered options

- `fetch <id>` into the current directory: an agent runs in the project, so the files would land
  there.
- A per-entity cache under `~/.cache/elport`: bounded, but it needs eviction and a `clean` command.
- Naming it `archive`: eLabFTW entities have an archived state, so the name reads as a remote change,
  and `git archive` makes a tarball, not a directory.
