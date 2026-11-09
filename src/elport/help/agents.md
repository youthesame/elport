# Acting for a user

You are running elport for someone. A push overwrites a record on a shared eLabFTW server that other people read,
so these rules say where to stop and ask.

## Before anything

- Use elport for every eLabFTW read and write. Do not call the eLabFTW API yourself, and do not script around a
  refusal.
- Find an entity with `elport list` or `elport list -q <term>`, and read it with `elport view <id>`. Never guess an
  `id` or edit one by hand: a wrong id overwrites the wrong entity. `new` and the first `push` assign it.
- `elport whoami` checks the credentials. Never print, echo, or write the API key into a file, a note, or a log.
  `elport login` asks for the key interactively, so leave it to the user.

## The push loop

Run `elport status`, then `elport push -n`, then `elport push` (see `elport help workflow`). `status`, `diff`, and
`push -n` send nothing, so run them freely. A push the user asked for needs no second confirmation once they pass.
Report the result, not each check.

## Ask the user first

- `--force`. It discards an edit someone made in the Web UI. When push says `remote changed`, run `elport pull`
  and `elport merge` instead. If you cannot tell whether the remote edit matters, show `elport diff` and ask.
- `--yes`. It skips the confirmations for widening `read`/`write` beyond the team and for uploads over 25 MiB. Show
  `elport push -n` and add `--yes` only after the user approves what it skips.
- `attachments:`. push warns when a path came in through the remote body, because someone else wrote it there. List
  it under `attachments:` only if the user wants that file uploaded.
- `read:` and `write:`. Leave them alone unless the user asks.

## Stop and tell the user

- A locked entity. push refuses it, even with `--force`. The user unlocks it in the Web UI.
- An HTML entity, written in the Web UI's rich text editor. clone, pull, and push refuse it. Read it with
  `elport view <id> -o DIR` into a scratch directory outside the project, and never change its `content_type`.
