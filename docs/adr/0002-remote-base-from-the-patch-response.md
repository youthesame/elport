# remote-base comes from the PATCH response, not a re-GET

push used to store remote-base by re-`GET`ting the entity after its body `PATCH`. It now takes the
entity the `PATCH` itself returns. That response *is* the server's stored form: on demo.elabftw.net
6.0.5 (2026-10-02) it was identical, body and every key, to an immediate `GET` across HTML, `&`,
tables, Japanese, code fences, title/category/status/permission changes, and an empty body. So the
two-form base invariant is unchanged: remote-base is still what the server stored, not the bytes we
sent.

Two reasons, in order of weight:

- **It is more correct.** A re-`GET` races with Web edits: an edit landing between our `PATCH` and
  the `GET` was silently adopted as our base, so the next push could not detect it. The `PATCH`
  response describes exactly what we wrote.
- **It is faster.** Every request costs a round trip (measured ~0.4 s with a reused connection,
  ~1 s without). Dropping one of six on a typical push is noticeable.

There is deliberately no fallback to a `GET`. An instance whose `PATCH` returns no entity fails the
existing `content_type == 2` check and aborts before state is saved, the same safe stop as an old
instance that ignores markdown mode. Measured on demo 6.0.5 only; re-measure on the demo when its
version changes.

## Consequences

- AGENTS.md's base invariant now says "the stored entity returned by the push `PATCH`".
- An interrupted push whose `PATCH` applied but whose response was lost resumes the same way the old
  "verify `GET` failed" case did (tests: `test_new_push_resumes_after_failure_without_force`).
- Considered and rejected at the same time: skipping the pre-`PATCH` recheck when nothing was
  uploaded (the window still spans prompts and the permission-narrowing `PATCH`), and an "up-to-date"
  no-op push (base equality does not prove remote metadata, tags, or attachment URLs are current).
