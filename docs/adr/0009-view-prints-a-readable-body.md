# `view` prints a readable body, not the stored bytes

`view` printed the body exactly as stored. For an HTML entity written in the Web UI, most of those
bytes are Word styles and `download.php` URLs that need a browser session. An agent spends tokens
reading them, and a person cannot use them. `view` now prints what `view -o` writes into
`report.md`, and the two differ only in whether the shown attachments are downloaded. This amends
[ADR-0008](0008-view-writes-a-snapshot-into-a-directory-the-caller-names.md), whose snapshot was a
copy of the body.

- **An HTML entity loses its attributes.** elport keeps `href`, `src`, `colspan`, and `rowspan`,
  unwraps `<span>`, drops comments, and collapses blank lines outside `<pre>`. Colour goes too.
  Turning a coloured span into `<strong>` would make most of a Word paste bold, because Word often
  sets `color: black` on every run. Markdown allows inline HTML, so the result is still `report.md`.
  On a Word-pasted body measured on demo 6.0.9 it kept 36% of the characters.
- **Upload URLs become file names** in either kind of body. elport matches a `download.php` URL to
  an attachment by `f` alone, relative or absolute, because the Web UI writes both shapes
  (ELABFTW-API, "Bodies written in the Web UI"). Links to other entities become absolute URLs.
  On stdout those files do not exist yet, so a line on stderr says `-o` downloads them.
- **A Markdown entity's body is otherwise unchanged.**
- **There is no `--raw`.** Only someone studying server normalization needs the stored bytes, and
  that work calls the API directly.

## Considered options

- **Convert HTML to Markdown with markdownify.** It kept 23% of the same body, against 36%, but adds
  BeautifulSoup as a dependency. The difference is mostly table tags.
- **Keep printing the stored bytes, or add `--raw`.** Nobody reading the body needs them.
