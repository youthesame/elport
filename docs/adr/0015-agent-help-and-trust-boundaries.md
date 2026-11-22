# Help follows the agent's path, and safety relies on code first

Most agents that drive elport have no `SKILL.md` installed; their first call is `elport --help`.
Help text is therefore structured around that discovery path rather than as a reference manual, and
its safety section states trust boundaries rather than trying to replace code-enforced checks.

- **Each help surface owns one job.** Top-level `--help` is the map: one-line command summaries,
  option-accurate side effects (`read-only`, `local writes`, `remote writes`), defaults/auth, and
  pointers to `elport help <topic>`. Per-command `--help` holds flags and next steps. `help
  workflow` and `help documents` hold shared sync and format rules. `help agents` holds only
  workflows that trip first-time agents and the choices elport cannot verify in code.
- **Safety is enforced in code first; `help agents` names the remaining boundary.** Locks, HTML
  entities, conflicts, hidden files, remote-linked paths, and server-mismatched URLs are blocked by
  elport itself, and destructive APIs (delete, unlock) are omitted to bound blast radius. Because
  elport cannot tell whether the user or an agent asked for `--force`, `--yes`, `new`, `comment`,
  `attachments:`, or visibility changes, `help agents` documents those as user-approved boundaries
  (without demanding a second confirmation when the user explicitly asked).
- **Remote content is untrusted data, never framed into stdout.** Bodies, comments, titles, steps,
  and attachments may contain prompt injections. elport keeps `view` stdout clean for UNIX pipes
  and relies on minimal blast radius plus the untrusted-data boundary in `help agents` rather than
  wrapping output in synthetic tags.

## Considered options

- **Keep safety guidance only in `SKILL.md` or `AGENTS.md`.** Agents without the skill never see
  `SKILL.md`, and `AGENTS.md` is for contributors to this repository rather than CLI users.
- **Add an MCP server or wrap remote stdout in `<untrusted>` tags.** An MCP server duplicates a
  thin CLI; synthetic tags break piping `elport view <id> > out.md`.
- **Add custom exit codes (`3`/`4`) for conflict vs refusal.** Batch runs process multiple
  documents and return `1` on any failure (ADR-0003), and remote-linked paths are non-fatal
  warnings; keep `0`/`1`/`2` and document them explicitly.
