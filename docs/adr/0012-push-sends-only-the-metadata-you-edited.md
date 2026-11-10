# push sends only the metadata you edited

pull writes `title`, `category`, `status`, `read`, and `write` into front matter, and push sent every one of them
back. So a key the user never touched still overwrote the remote:

- A permission or category changed in the Web UI after the last pull was reverted by the next push, without a
  warning, or stopped it (`permission widening requires confirmation`, `write: owner would revoke your own access`).
- push looked every category and status up in the active team's catalog, so it failed with `category not found`
  on another team's entity, or after the category was renamed in the Web UI.

The conflict check compares only the body, so none of this showed up as a conflict.

- **push sends a synced key only when it differs from the metadata base**, the remote values recorded by the
  last push or pull (`meta_base`). It neither looks up nor sends the others. With no base (a new entity, or a
  state saved before `meta_base` existed) it sends every key present, as before.
- `status` reports a permission change only for an edited key, so it describes what push would do.
- The base recorded after push is still the front matter, so a Web-side change to a key push left alone is a
  remote change on the next pull, which takes it.

## Considered options

- **Compare each key with the remote and stop on a difference.** That adds a metadata conflict state to resolve,
  when pull already merges metadata three-way against the same base.
- **Stop writing these keys on pull.** The user could no longer see or edit them in front matter.
