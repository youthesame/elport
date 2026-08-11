# elab — a git-like sync CLI for eLabFTW

[English](README.md) | [日本語](README_JA.md)

Write lab notes locally in Markdown — inline HTML like `<figure>` is fine — then `push`/`pull` them, along with
every figure and data file the body references, to eLabFTW entities. **Local is the source of truth.**

## Install

Python 3.10+, installed as a `uv` tool (provides the `elab` command):

```sh
uv tool install git+https://github.com/youthesame/elab
```

## The core idea

One local file maps to one eLabFTW entry: Markdown with a YAML front matter block (inline HTML allowed). On `push`,
elab uploads every real local file the body references — in any notation — and swaps each path for its real eLabFTW
URL. The body goes up as raw Markdown, so `<figure>` and `$...$` render as-is. To link another entry instead of
uploading, just write its eLabFTW URL. Code fences, inline code, and HTML comments are never parsed.

## Quick start

```sh
elab login labA                       # store base_url (config) + api_key (OS keyring), interactively
elab new "260809 CRISPR titration"    # create the entity, scaffold report.md with elab_id
# ...edit report.md, drop fig1.png / data.csv next to it...
elab status                           # what would sync? (read-only)
elab push                             # upload references + push the body
```

## Commands

Default document is `report.md`; any name works. Exit code `0` on success, `1` on failure.

| Command | Summary |
|---|---|
| `elab push [<doc>]` | Push `<doc>` to one entity. Creates it (writing `elab_id` back) if unset. Runs mode + conflict checks first. |
| `elab pull [<doc>]` | Fetch the body, reverse-transclude URLs back to local paths, download referenced files. |
| `elab status [<doc>]` | Side-effect-free: is local changed? was the remote edited? which files upload? what mode? |
| `elab diff [<doc>]` | Source-form diff. Default local ↔ remote; `--base` for local ↔ last push. Never sends. |
| `elab merge [<doc>]` | After a conflict, 3-way `.base.md`/`.remote.md` into `<doc>` via `git merge-file`. Local-only; git optional. |
| `elab comments [<doc>]` | Print the remote comment thread (terminal only; never written into the body). |
| `elab comment [<doc>] "<text>"` | Post one comment to the entity (no edit/delete — use the Web UI). |
| `elab new "<title>" [--entity experiments\|items] [--profile <name>] [-o <doc>]` | Create an entity + scaffold front matter. |
| `elab whoami [--profile <name>]` | Auth check: user, team + role, API-key read/write, server version, scopes. |
| `elab login [<profile>]` | Store base_url → `config.toml`, api_key → OS keyring. Prompts; the key is not echoed. |
| `elab logout [<profile>]` | Remove the stored api_key for a profile; keeps base_url. |
| `elab profile [use <name>]` | List profiles (default marked), or set the default profile. |

Options: `-n/--dry-run` (push rehearsal, no send), `--profile <name>`, `-f/--force` (push over a changed remote — the
Web-side change is lost), `-y/--yes` (skip the confirmation when widening `read`/`write` beyond your team),
`--entity {experiments,items}`.

> pull writes referenced files back by **basename only** — a subdirectory path like `assets/fig.png` is flattened to
> `fig.png`.

## Document format (front matter)

A YAML block at the top (generated/completed on push if absent):

```markdown
---
elab_id: 42                   # entity ID (auto-filled on creation)
entity: experiments           # experiments | items (default experiments)
title: "experiment title"
tags: [CRISPR, PCR]           # optional; add-only (remove tags in the Web UI)
category: Molecular Biology   # optional; ID or existing category name (elab never creates categories)
status: Running               # optional; ID or existing status name (elab never creates statuses)
profile: labA                 # optional; destination profile
read: team                    # optional; owner | owner+admin | team | account | public
write: owner                  # optional; same scale
---

# Body — Markdown, inline HTML allowed ...
```

- Holds **only** `elab_id` + human metadata + optional `profile`; base and hashes live in state. Front matter is
  stripped before the body is sent.
- `title` / `category` / `status` / `read` / `write` are reflected **only when present** — omit a key and elab
  leaves that remote value untouched. `read`/`write` set the eLabFTW base visibility only, so individual Web-UI
  grants are preserved; widening beyond your team asks for confirmation (`-y` skips; non-interactive needs `-y`).
- If front matter and CLI disagree on profile / entity / elab_id, elab **stops** rather than guessing.

## Conflicts

Since you may also edit the body in the Web UI, `push` compares the current remote against the stored base first:

- **unchanged** → proceeds.
- **changed** → aborts. elab writes `<name>.base.md` (ancestor) and `<name>.remote.md`; run `elab merge` to 3-way
  them into your file, resolve any `<<<<<<<`/`>>>>>>>` markers, then push. (push refuses a body that still has
  markers.)
- **no base on this machine** → aborts; `elab pull` first, or `--force` to overwrite blind.

`--force` discards the Web-side change — use it deliberately. eLabFTW keeps server-side history recoverable from the
Web UI as a safety net. elab keeps no local history of its own; want per-edit history? `git init` your notes folder —
the files are plain Markdown.

## Configuration & auth

**No credentials in the project.** `elab login` stores the API key in your OS keyring (Keychain / Credential
Manager / Secret Service); base_url and other non-secrets live in `~/.config/elab/config.toml` (mode `600`, key
never printed).

```toml
# ~/.config/elab/config.toml
default_profile = "labA"

[profiles.labA]
base_url   = "https://elab-a.example.org"
verify_ssl = true
```

Credentials resolve **env → keyring+config → plaintext**: `ELABFTW_BASE_URL`+`ELABFTW_API_KEY` for CI, the keyring
pair as the normal default, then a warned plaintext-in-config fallback only when no keyring backend exists. Profiles
layer like settings.json (`config.toml` → `<project>/.elab.toml` → `<dir>/.elab.toml`, one per team); your first
`elab login` becomes the default, `elab profile use <name>` switches it, or set `profile:` per note. `.elabignore`
excludes referenced files `.gitignore`-style, additive across those layers.

## Learn more

- **Why it works this way** → [docs/DESIGN.md](docs/DESIGN.md)
- **How eLabFTW's API actually behaves** → [docs/ELABFTW-API.md](docs/ELABFTW-API.md)
- **The behavioral contract** → the test suite (`tests/`) is authoritative
- **Driving elab from an AI agent** → [skills/elab/SKILL.md](skills/elab/SKILL.md)

## Development

Structure: `client.py` (API wrapper) / `transclude.py` (both directions) / `config.py` / `state.py` (base) /
`sync.py` (push/pull/status/diff) / `cli.py`. **The tests are the authoritative behavioral contract — change tests
first.** Live API behavior can be checked against <https://demo.elabftw.net>.

## Related

- [elab-doc-sync](https://github.com/Kosaku-Noba/elab-doc-sync)
- [elAPI](https://github.com/uhd-urz/elAPI)

## License

MIT
