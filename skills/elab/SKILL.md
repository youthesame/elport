---
name: elab
description: Use the `elab` CLI to sync a local Markdown/HTML lab note to eLabFTW (push/pull/status/diff), never by hand-writing eLabFTW REST calls. Trigger whenever you are about to upload, push, pull, or fetch an experiment note or its attachments to/from eLabFTW, or reach for the eLabFTW API to do so. Local files are the source of truth; elab is git-for-eLabFTW.
---

# elab — git-for-eLabFTW: push/pull a local note, never hand-drive the API

One doc = one entity: `report.md` (any name) with YAML frontmatter (`elab_id`, `entity`,
`title`, `tags`, `category`, optional `read`/`write`). elab uploads every real local file the
body references and rewrites the path to its eLabFTW URL. **Local is the source of truth. Never
write eLabFTW API calls yourself — use elab.**

## Cheatsheet

```sh
elab status                    # local clean/dirty · remote changed? · uploads · mode  (read-only)
elab diff                      # local ↔ remote, source form (read-only, no send)
elab diff --base               # local ↔ last push (what YOU changed; zero normalization noise)
elab push                      # upload refs + PATCH body as markdown (content_type:2)
elab push -n                   # --dry-run: full rehearsal — upload + path→URL plan, no writes
elab push report.md            # doc defaults to report.md; any name works
elab pull                      # fetch body, reverse-transclude URLs → local paths, download files
elab merge                     # after a conflict: git merge-file .base.md/.remote.md → doc (local)
elab comments                  # print the remote comment thread (read-only, terminal only)
elab comment "text"            # post one comment (no edit/delete)
elab new "title" --entity experiments -o report.md   # create entity + scaffold frontmatter
elab whoami                    # auth check: user, team+role, API-key read/write, server version, scopes
elab profile [use <name>]      # list profiles (default marked), or switch the default
elab login [profile]           # store base_url (config) + api_key (OS keyring); prompts, no echo
```

## Preflight discipline

The loop is **`status` → (`diff`) → `push`**, mirroring git. Do not push blind.

- `status`, `diff`, and `push -n` **never send** — they are free. Run the cheapest one that
  answers the question: `status` for the summary, `diff` for *what* changed in source form,
  `push -n` for *what the push would do* (upload list + path→URL plan + abort conditions).
- Read `status` before every push: `local: clean` means nothing to send; `remote: changed`
  means the Web UI was edited — stop and resolve (see Safety) before pushing.
- Don't narrate the preflight. Run the checks, then act; report the result, not the method.

## Reference

- **Flags**: `--profile <name>` (team/instance), `--entity experiments|items` (default
  `experiments`; frontmatter wins). `push` adds `-n/--dry-run`, `-f/--force`, `-y/--yes`.
  Per-command detail: `elab <cmd> --help`.
- **Attachments**: reference a **local path** to attach-and-upload; reference a full **eLabFTW
  http URL** to only link another entity (not uploaded). elab skips schemed URLs, absolute
  paths, `..` escapes, and code-fence/inline-code content.
- **Permissions**: frontmatter `read:`/`write:` (`owner|owner+admin|team|account|public`) set
  the eLabFTW base visibility — only when present, and only the base level (Web-UI individual
  grants are preserved). Widening to `account`/`public` needs confirmation or `-y`.

## Safety — writes are shared and hard to undo

eLabFTW entities are shared; a push overwrites the server body, and `--force` discards whatever
is on the remote. Treat these like any irreversible outward action.

- **Never `--force` past a conflict without the user's explicit consent.** `remote changed; use
  pull or --force` (or `base unavailable; run pull first or use --force`) means the server body
  diverged from your last push — someone edited the Web UI, or this machine has no base.
  Normal path: `elab pull`; elab writes `<name>.base.md` (ancestor) + `<name>.remote.md`, then
  `elab merge` 3-ways them into the note, resolve any `<<<<<<<` markers, and `push` (push
  refuses a body that still carries markers). If you can't tell whether the remote change
  matters, show `elab diff` and ask — don't guess, and don't `--force` to clear the error.
- **Keep secrets out of the note and repo.** API keys live in the OS keyring via `elab login`;
  base_url in `~/.config/elab/`. Never print, echo, or write a key into a file, note, or log.
- **`elab_id` is server-assigned** by `new` and the first `push`. Never invent or hand-edit it —
  a wrong id overwrites the wrong entity.
- **Markdown mode is required.** If push aborts with `remote entity is not in markdown mode`,
  surface it rather than working around it — the target instance needs its markdown editor on.
