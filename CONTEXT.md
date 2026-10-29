# elport

A git-like sync tool between local lab notes and eLabFTW entries. The local document is the source
of truth.

## Language

**Document**:
A local Markdown file with front matter that maps to exactly one entity.
_Avoid_: note, page, file

**Entity**:
An eLabFTW experiment or resource, the remote counterpart of a document.
_Avoid_: entry, record

**Reference**:
A local path written in the document body, in any notation, that push uploads and replaces with
the attachment's URL.
_Avoid_: embed, link (when the target is a file)

**Document link**:
A reference to another document, sent as that entity's URL and never uploaded.

**Attachment**:
A file stored on an entity (an eLabFTW "upload").
_Avoid_: upload (as a noun for the stored file), file

**Listed attachment**:
A file named in the document's `attachments:` front matter, uploaded without being referenced in
the body.
_Avoid_: unlinked attachment, extra file

**Base**:
What a document and its entity looked like at the last sync, kept in two forms (remote-base and
local-base) so each side is compared only against its own form.
