# elab — a git-like sync CLI for eLabFTW

[English](README.md) | [日本語](README_JA.md)

Write lab notes locally in Markdown — inline HTML like `<figure>` is fine — then `push`/`pull` them, along with
every figure and data file the body references, to eLabFTW entities. **Local is the source of truth.**

- **Why it works this way** → [docs/DESIGN.md](docs/DESIGN.md)
- **How eLabFTW's API actually behaves** → [docs/ELABFTW-API.md](docs/ELABFTW-API.md)
- **The behavioral contract** → the test suite (`tests/`) is authoritative
- **Driving elab from an AI agent** → [skills/elab/SKILL.md](skills/elab/SKILL.md)

## Install

Python 3.10+, installed as a `uv` tool (provides the `elab` command):

```sh
uv tool install git+https://github.com/youthesame/elab
```

## The core idea

One local file maps to one eLabFTW entry: Markdown with a YAML front matter block (inline HTML is allowed). On `push`,
elab collects every real local file the body references — in any notation — uploads them, and swaps each path for
its real eLabFTW URL. `width`, alt text, and `<figcaption>` survive; the body goes up as **raw Markdown**, so
`<figure>` and `$...$` render as-is.

To link another entry instead of uploading, just write its eLabFTW URL. Code fences, inline code, and HTML comments
are never parsed.

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
| `elab push [<doc>]` | Push `<doc>` to one entity. Creates the entity (writing `elab_id` back) if unset. Runs mode + conflict checks first. |
| `elab pull [<doc>]` | Fetch the body, reverse-transclude URLs back to local paths, download referenced files. |
| `elab status [<doc>]` | Side-effect-free: is local changed? was the remote edited? which files upload? what mode? |
| `elab diff [<doc>]` | Source-form diff. Default local ↔ remote; `--base` for local ↔ last push. Never sends. |
| `elab new "<title>" [--entity experiments\|items] [--profile <name>] [-o <doc>]` | Create an entity + scaffold front matter. |
| `elab whoami [--profile <name>]` | Auth check; shows user and active team. |
| `elab login [<profile>]` | Store base_url → `config.toml`, api_key → OS keyring. Prompts; the key is not echoed. |

Options: `-n/--dry-run` (push rehearsal, no send), `--profile <name>` (resolution: front matter → CLI → default),
`-f/--force` (push over a changed remote — the Web-side change is lost), `--entity {experiments,items}` (front
matter wins).

> Reverse transclusion (pull) writes files back by **basename only** — a subdirectory path like `assets/fig.png` is
> flattened to `fig.png`.

## Document format (front matter)

Put a YAML block at the top (generated/completed on push if absent):

```markdown
---
elab_id: 42            # entity ID (auto-filled on creation)
entity: experiments    # experiments | items (default experiments)
title: "260806 experiment title"
tags: [CRISPR, PCR]    # optional; add-only
category: Molecular Biology   # optional (ID or existing category name)
profile: labA          # optional; destination profile
---

# Body — Markdown, inline HTML allowed ...
```

- Holds **only** `elab_id` + human metadata + optional `profile`. Base and hashes live in state, not here.
- `title`/`category` are reflected; **`tags` are add-only** (remove tags in the Web UI). Front matter is stripped
  before the body is sent.
- If front matter and CLI disagree on profile / entity / elab_id, elab **stops** rather than guessing.

## Conflicts

Since you may edit the body in the Web UI, `push` compares the current remote against the stored base first:

- **unchanged** → proceeds.
- **changed** → aborts (`remote changed; use pull or --force`). `elab pull`, reconcile with your git tooling (elab
  emits source-form files for `git merge-file`), then push.
- **no base on this machine** → aborts; `elab pull` first, or `--force` to overwrite blind.

`--force` discards the Web-side change — use it deliberately. eLabFTW keeps server-side history recoverable from the
Web UI as a safety net (not on every save — see [docs/ELABFTW-API.md](docs/ELABFTW-API.md)).

## Configuration & auth

**No credentials in the project.** API key → OS keyring (Keychain / Credential Manager / Secret Service), set via
`elab login`. base_url and non-secrets → `~/.config/elab/config.toml` (mode `600`). The key is never printed.

Resolution order: (1) env `ELABFTW_BASE_URL` + `ELABFTW_API_KEY` (both required together, for CI); (2) keyring +
`config.toml` (normal default); (3) no keyring backend → prompted toward env, with an explicit warning if it falls
back to a plaintext key in config.

Profiles override in layers, like settings.json: `~/.config/elab/config.toml` (user) → `<project>/.elab.toml` →
`<dir>/.elab.toml`. Each holds base_url etc.; keys always go to the keyring, one profile per team.

```toml
# ~/.config/elab/config.toml
default_profile = "labA"

[profiles.labA]
base_url   = "https://elab-a.example.org"
verify_ssl = true
```

`.elabignore` gives `.gitignore`-style exclusion for referenced files, additive across the layers above.

## Development

Structure: `client.py` (API wrapper) / `transclude.py` (both directions) / `config.py` / `state.py` (base) /
`sync.py` (push/pull/status/diff) / `cli.py`. **The tests are the authoritative behavioral contract — change tests
first.** Live API behavior can be checked against <https://demo.elabftw.net>; confirm version-dependent behavior on
the real target instance too.

## Related

- [elab-doc-sync](https://github.com/Kosaku-Noba/elab-doc-sync)

## License

MIT
