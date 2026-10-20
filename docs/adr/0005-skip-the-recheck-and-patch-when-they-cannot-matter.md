# push skips the pre-PATCH recheck and the PATCH when they cannot matter

[ADR-0002](0002-remote-base-from-the-patch-response.md) rejected both. Its reasons are now checked
directly instead:

- **The recheck `GET` runs only when something slow sits between the first read and the `PATCH`**:
  the body references a file (uploads are listed and local files hashed, possibly uploaded and
  confirmed) or a permission actually changes (a widening prompt, a narrowing `PATCH`). Otherwise
  that gap is local work only, and a second read costs a round trip without closing a real window.
- **An unchanged push sends no `PATCH`** when the local body equals local-base, the remote body
  equals remote-base, no file is uploaded, the document links resolve as last recorded, every other
  payload field (title, category, status, permissions, `content_type`) already equals the remote, and
  the remote body has no attachment URL that matches none of the entity's uploads (a Web-side
  delete and re-upload). Tags are added either way. With no `PATCH` there is nothing to overwrite,
  so there is no recheck either.

Why: a folder push of unchanged notes went from 4 requests per document to 1 (demo 6.0.8,
2026-10-20), and a skipped `PATCH` adds no revision on the server.

## Consequences

- The remote-base saved by a skipped push is the body of the first `GET`, which equals the stored
  remote-base by the condition above.
- Any field the check does not compare forces a `PATCH`, so a mismatch costs a request, never a
  missed update.
