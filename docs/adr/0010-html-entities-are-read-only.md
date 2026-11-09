# HTML entities are read-only

An entity written in the Web UI's rich text editor stores HTML (`content_type` 1). clone wrote that
HTML into `report.md`, and the next push sent it back with `content_type` 2. eLabFTW 6.x does not
render raw HTML in a Markdown body, so on demo 6.0.9 the Web UI then showed an empty page until
`content_type` went back to 1. Upstream treats the two kinds as not interchangeable and now disables
the editor switch on any non-empty entry ([elabftw#7708](https://github.com/elabftw/elabftw/pull/7708)).
elport therefore reads HTML entities and never syncs them.

- **`view` and `view -o` read them** ([ADR-0009](0009-view-prints-a-readable-body.md)).
- **clone, pull, and push refuse an HTML entity with a non-empty body**, including one held by a
  working copy cloned before this rule. The message points to `view -o`. An empty one passes, as
  eLabFTW lets anyone switch an empty entry.
- **`clone --all` writes an HTML entity as a snapshot** in the directory a clone would have used,
  `<entity>/<id>-<title>/`. A snapshot has no front matter, so on a re-run `clone --all` counts any
  `<entity>/<id>-*` directory as present. A re-run does not refresh it. Delete the directory to fetch
  it again.

## Considered options

- **Convert the HTML to Markdown on clone.** push would then switch the entity to Markdown, the same
  switch upstream now blocks in the Web UI, and the author would find a different editor on their
  own note.
- **Give the snapshot front matter with only `id`.** Directory-wide push and status would take it
  for a working copy, and `--force` past the missing base would overwrite the entity with the
  stripped body.
