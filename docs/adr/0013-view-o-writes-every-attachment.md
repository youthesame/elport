# `view -o` writes every attachment, not only those the body shows

[ADR-0008](0008-view-writes-a-snapshot-into-a-directory-the-caller-names.md) kept attachments the body does not
show on the server, because they are often large raw data. That left no read-only way to get them: `fetch` needs
a working copy, so an agent asked to look at a colleague's procedure and data ran `clone` and then `fetch`, and
left a working copy that can push to someone else's entity.

- **`view -o` writes every attachment** the entity has, beside the body, under the same names `fetch` uses. A
  caller who names a directory wants the entity on disk to read, and an attachment the body never links is still
  part of it.
- Hidden attachments are still never placed, and an attachment named `report.md` still refuses the snapshot,
  now whether or not the body shows it.
- stderr still names the attachments the body does not show, so the reader knows which files the body never
  mentions.

## Considered options

- **A flag such as `view -o DIR --all`.** It keeps the default light, but an agent has to notice the flag, and
  the reason for this change is that agents missed `view -o` itself.
- **`fetch <id> -o DIR` without a working copy.** It splits reading into two commands an agent must choose
  between.
