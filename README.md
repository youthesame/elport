# elab

**Git-like sync CLI for eLabFTW.** Write lab notes locally in Markdown/HTML (with referenced figures and data
files), then `push`/`pull` them to eLabFTW entities. **Local is the source of truth.** Not Markdown-only — it
handles md/HTML bodies plus arbitrary referenced attachments.

- **Why it works this way** → [docs/DESIGN.md](docs/DESIGN.md)
- **How eLabFTW's API actually behaves** → [docs/ELABFTW-API.md](docs/ELABFTW-API.md)
- **The behavioral contract** → the test suite (`tests/`) is authoritative
- **Driving elab from an AI agent** → [skills/elab/SKILL.md](skills/elab/SKILL.md)

## Install

Python 3.10+, installed as a `uv` tool (provides the `elab` command):

```sh
uv tool install elab
```

## The core idea

One document = one eLabFTW entity. A note is a Markdown/HTML file (default `report.md`) with a YAML front matter
block. On `push`, elab finds every **real local file the body references** — regardless of notation (`<img src>`,
`[text](path)`, `![alt](path)`, `href`) — uploads each, and **replaces the local path with the real eLabFTW URL**
(only the URL; `width`, alt text, `<figcaption>` are preserved). The body is sent as **raw Markdown**
(`content_type:2`); `<figure>` and `$...$` math render on eLabFTW as-is.

To only **link** another entity (no upload), reference it by its full eLabFTW `http(s)` URL — schemed URLs are
skipped. Paths inside code fences, inline code, and HTML comments are never parsed.

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

# Body (Markdown / HTML may be mixed) ...
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
