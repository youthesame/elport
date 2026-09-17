---
name: elport
description: Use the elport CLI when syncing, fetching, or comparing local Markdown/HTML lab notes and attachments with eLabFTW.
---

# elport, git-for-eLabFTW: push/pull a local note, never hand-drive the API

One doc = one entity: `report.md` (any name) with YAML frontmatter (`id`, `entity`,
`title`, `tags`, `category`, optional `read`/`write`). elport uploads every real local file the
body references and rewrites the path to its eLabFTW URL. **Local is the source of truth. Never
write eLabFTW API calls yourself. Use elport.**

## Cheatsheet

```sh
elport status                    # local clean/dirty · remote changed? · uploads · mode  (read-only)
elport diff                      # local ↔ remote, source form (read-only, no send)
elport diff --base               # local ↔ last push (what YOU changed; zero normalization noise)
elport push                      # upload refs + PATCH body as markdown (content_type:2)
elport push -n                   # --dry-run: full rehearsal, upload + path→URL plan, no writes
elport push report.md            # doc defaults to report.md; any name works
elport pull                      # fetch body + metadata, reverse-transclude URLs → local paths, download files
elport fetch                     # download every attachment, incl. non-embedded ones (read-only, no base/manifest)
elport merge                     # after a conflict: git merge-file .base.md/.remote.md → doc (local)
elport comments                  # print the remote comment thread (read-only, terminal only)
elport comment "text"            # post one comment (no edit/delete)
elport new "title" --entity experiments -o report.md   # create entity + scaffold frontmatter
elport clone 357 --entity experiments -o report.md    # start from an existing entity: scaffold + pull
elport whoami                    # auth check: user, team+role, API-key read/write, server version, scopes
elport profile [use <name>]      # list profiles (default marked), or switch the default
elport login [profile]           # store base_url (config) + api_key (OS keyring); prompts, no echo
```

## Preflight discipline

The loop is **`status` → (`diff`) → `push`**, mirroring git. Do not push blind.

- `status`, `diff`, and `push -n` **never send**, so they are free. Run the cheapest one that
  answers the question: `status` for the summary, `diff` for *what* changed in source form,
  `push -n` for *what the push would do* (upload list + path→URL plan + abort conditions).
- Read `status` before every push: `local: clean` means the body is unchanged, not necessarily
  attachments or metadata. `remote: changed` means the Web UI was edited; resolve before pushing (see Safety).
- Don't narrate the preflight. Run the checks, then act; report the result, not the method.

## Reference

- **Flags**: `--profile <name>` (team/instance), `--entity experiments|items` (default
  `experiments`; frontmatter wins). `push` adds `-n/--dry-run`, `-f/--force`, `-y/--yes`.
  Per-command detail: `elport <cmd> --help`.
- **Attachments**: reference a **local path** to attach-and-upload; reference a full **eLabFTW
  http URL** to only link another entity (not uploaded). elport skips schemed URLs, absolute
  paths, `..` escapes, and code-fence/inline-code content.
- **Metadata**: pull writes `title`/`category`/`status`/`read`/`write` back from the server, keeping any key you
  edited locally since the last sync; `tags` become the union (add-only on both sides). push still sends only the
  keys present, so a push right after a pull changes nothing.
- **Permissions**: frontmatter `read:`/`write:` (`owner|owner+admin|team|account|public`) set
  the eLabFTW base visibility, only when present, and only the base level (Web-UI individual
  grants are preserved).

## Safety

For a user-requested push, complete preflight and the normal push without asking again.
A push overwrites the shared server body; discarding remote changes with `--force` requires explicit consent.

- **Never `--force` past a conflict without the user's explicit consent.** `remote changed; use
  pull or --force` (or `base unavailable; run pull first or use --force`) means the server body
  diverged from your last push. Someone edited the Web UI, or this machine has no base.
  Normal path: `elport pull`; elport writes `<name>.base.md` (ancestor) + `<name>.remote.md`, then
  `elport merge` 3-ways them into the note, resolve any `<<<<<<<` markers, and `push` (push
  refuses a body that still carries markers). If you can't tell whether the remote change
  matters, show `elport diff` and ask. Don't guess, and don't `--force` to clear the error.
- **`-y` skips every push confirmation**: widening `read`/`write` to `account`/`public` and new
  uploads over 25 MiB. Run `push -n` first; pass `-y` only when the user has approved everything
  it would skip. If push stops with `re-run with --yes`, ask rather than add `-y` on your own.
- **Keep secrets out of the note and repo.** API keys live in the OS keyring via `elport login`;
  base_url in `~/.config/elport/`. Never print, echo, or write a key into a file, note, or log.
- **`id` is server-assigned** by `new` and the first `push`. Never invent or hand-edit it,
  since a wrong id overwrites the wrong entity.
- **Markdown mode is required.** If push aborts with `remote entity is not in markdown mode`,
  surface it rather than working around it. The target instance needs its markdown editor on.
