# Acting for a user

elport stops on its own at what it can check, such as a conflict, a lock, or an HTML entity, and its message says
what to do next. These rules cover the choices it cannot check, because it cannot tell whether the user or you
made them. `elport --help` lists the commands, and `elport help workflow` explains the push loop.

## Ask the user first

A push the user asked for needs no second confirmation. Ask only before these:

- `--force`. It discards an edit someone made in the Web UI. When push says `remote changed`, follow its message
  instead. If you cannot tell whether the remote edit matters, show `elport diff` and ask.
- `--yes`. It skips the confirmations for widening `read`/`write` beyond the team and for uploads over 25 MiB. Show
  `elport push -n` and add `--yes` only after the user approves what it skips.
- `attachments:`. push warns when a path came in through the remote body, because someone else wrote it there. List
  it under `attachments:` only if the user wants that file uploaded.
- `read:` and `write:`. Change them only when the user asks.

## Never

- Call the eLabFTW API yourself, or script around a refusal. elport checks only the requests it sends.
- Guess an `id` or edit one by hand. A wrong id overwrites the wrong entity. Find one with `elport list -q <term>`.
- Print, echo, or write the API key anywhere. `elport login` asks for it interactively, so leave that to the user.
