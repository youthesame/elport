# Documents

A document is a Markdown file that starts with a YAML front matter block, and it maps to one entity. Inline HTML
is allowed.

```markdown
---
id: 42                        # assigned by new or the first push; never write it yourself
entity: experiments           # experiments | items (default experiments)
title: "experiment title"     # defaults to the file name on creation
tags: [CRISPR, PCR]           # add-only; remove tags in the Web UI
category: Molecular Biology   # ID or existing name; elport never creates one
status: Running               # ID or existing name; elport never creates one
profile: labA                 # which configured server to use
read: team                    # owner | owner+admin | team | account | public
write: owner                  # same scale
attachments: [data/raw.csv]   # files to upload without a link in the body
---
```

push sends a key only when you edited it since the last push or pull, so a key you left alone, or removed,
keeps the remote value. pull writes `title`, `category`, `status`, `read`, and `write` back from the server, but
keeps a key you edited and have not pushed.
`tags` merge both ways. `read` and `write` set the base visibility only, so grants made in the Web UI stay.

push fills in a missing block and strips it from the body it sends. The base and its hashes live in
`~/.config/elport/state/`, one file per entity, never in the project.

## References

push uploads every local file the body references, in any notation (`![](fig.png)`, `[data](data.csv)`,
`<img src="fig.png">`), and puts the attachment's URL in the copy it sends. Your file keeps the path.

- Relative paths, `..`, and absolute paths all work.
- elport skips URLs with a scheme, hidden files and directories (any dot-prefixed component), and anything inside
  code fences, inline code, or HTML comments.
- `.elportignore` excludes files inside the document directory, gitignore-style.
- A path that came in through a remote body is never uploaded unless it is listed under `attachments:`. push warns
  about it.

## Attachments not shown in the body

List raw data or spectra under `attachments:`. push uploads them, and fails on an entry that is missing, hidden,
ignored, or a directory. elport never deletes an attachment. pull does not bring these down; `elport fetch` does, and `elport view <id> -o DIR` does without a working copy.

view and pull name on stderr what the entity holds outside its body: attachments it does not show, compounds,
steps, and comments.

## Links to other notes

Link another document by its local path, as in `[previous](../exp12/report.md)`. push sends that entity's URL and
never the file. The target needs an `id`, so push it first. A full eLabFTW URL works too.
