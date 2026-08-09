---
name: elab
description: Use the `elab` CLI to sync a local Markdown/HTML lab note to eLabFTW (push/pull/status/diff), never by hand-writing eLabFTW REST calls. Trigger whenever you are about to upload, push, pull, or fetch an experiment note or its attachments to/from eLabFTW, or reach for the eLabFTW API to do so. Local files are the source of truth; elab is git-for-eLabFTW.
---

# elab — git-for-eLabFTW: push/pull a local note, never hand-drive the API

One doc = one entity. A note is `report.md` (any name) with YAML frontmatter
(`elab_id`, `entity`, `title`, `tags`, `category`). elab uploads every real
local file the body references and rewrites the path to the eLabFTW URL.
**Local is the source of truth. Never write eLabFTW API calls yourself — use elab.**

## Cheatsheet

```sh
elab status                    # local clean/dirty · remote changed? · uploads · mode  (read-only)
elab diff                      # local ↔ remote, in source form (read-only, no send)
elab diff --base               # local ↔ last push (what YOU changed; zero normalization noise)
elab push                      # upload refs + PATCH body as markdown (content_type:2)
elab push -n                   # --dry-run: full rehearsal, uploads/replacement plan, no writes
elab push report.md            # doc defaults to report.md; any name works
elab pull                      # fetch body, reverse-transclude URLs → local paths, download files
elab new "260809 title" --entity experiments -o report.md   # create entity + scaffold frontmatter
elab whoami                    # confirm auth + active team
elab login [profile]           # store base_url (config) + api_key (OS keyring); prompts, no echo
```

Common flags: `--profile <name>` (team/instance), `--entity experiments|items`
(default `experiments`; frontmatter wins). `push` adds `-n/--dry-run`, `-f/--force`.
Per-command detail: `elab <cmd> --help`.

## Workflow discipline

The loop is **`status` → (`diff`) → `push`**, mirroring git. Do not push blind.

- Before any push, run `status`. Read it: `local: clean` means nothing to send;
  `remote: changed` means the Web UI was edited — stop and resolve first (below).
- `diff` shows *what* changed in source form; `push -n` shows *what the push would do*
  (upload list + path→URL plan + abort conditions). `status` is the summary. Use the
  cheapest one that answers the question; they never send.
- Attachments: reference a **local path** to attach-and-upload; reference a full
  **eLabFTW http URL** to only link another entity (not uploaded). elab skips
  schemed URLs, absolute paths, `..` escapes, and code-fence/inline-code content.

## Conflicts

`remote changed; use pull or --force` or `base unavailable; run pull first or use
--force` means the server body diverged from your last push — someone edited the Web
UI, or this machine has no base to compare against. `--force` overwrites the server
with your local body, **discarding whatever is on the remote**. That is a destructive,
hard-to-undo change to someone else's work, so treat it like any irreversible outward
action: **use `--force` only with the user's explicit go-ahead.**

- Normal path: `elab pull` to bring the remote in, reconcile it with your local note
  (elab emits source-form files you can `git merge-file`), then `push`.
- If you can't tell whether the remote change matters, show `elab diff` and ask —
  don't guess, and don't reach for `--force` just to clear the error.

How you reconcile is your call; the one firm line is consent before `--force`.

## Invariants an agent must not break

The detailed contract lives in elab's tests. This is the safety-critical subset a
*using* agent could actually violate:

- **Never `--force` past a conflict without the user's explicit consent** — it
  discards Web-side edits that live only on the server.
- **Keep secrets out of the note and the repo.** API keys live in the OS keyring via
  `elab login`; base_url in `~/.config/elab/`. Never print, echo, or write a key into
  a file, note, or log.
- **`elab_id` is server-assigned** by `new` (and by the first `push`). Never invent or
  hand-edit it — a wrong id overwrites the wrong entity.
- **Markdown mode is required.** If push aborts with `remote entity is not in markdown
  mode`, surface it rather than hunting for a workaround; the target instance needs
  its markdown editor enabled.
