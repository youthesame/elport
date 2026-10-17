# push refuses a locked entity, even with --force

eLabFTW lets the owner keep writing to an entity they locked: on demo 6.0.8 (2026-10-17), a body
`PATCH` on a locked experiment returned 200 and changed the body. elport therefore checks `locked`
on the `GET` it already makes and stops before uploading anything. `--force` does not override this:
a lock records that the note is final, and elport deliberately has no command that locks, unlocks,
or timestamps (no `seal`). Unlocking is a Web UI action. `status` shows the lock, and pull/fetch
are unaffected.
