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

**HTML entity**:
An entity whose body is HTML, as the Web UI's rich text editor writes it. elport reads it and never
syncs it, so it never has a document.
_Avoid_: rich-text entity

**Reference**:
A local path written in the document body, in any notation, that push uploads and replaces with
the attachment's URL.
_Avoid_: embed, link (when the target is a file)

**Remote-linked path**:
A reference that appears in a remote body. Push never uploads the file it names unless it is a listed
attachment.
_Avoid_: foreign link

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

**Working copy**:
The one document that holds an entity's base on this machine.
_Avoid_: checkout, clone (as a noun)

**Snapshot**:
An entity's body in readable form, with the attachments it shows. It has no front matter and no
base, so it is never a document.
_Avoid_: export, archive
