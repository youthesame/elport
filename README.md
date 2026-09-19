# elport: a git-like sync CLI for eLabFTW

[English](README.md) | [日本語](README_JA.md)

Write lab notes locally in Markdown, with inline HTML like `<figure>` where you want it. Then `push`/`pull` them,
along with every figure and data file the body references, to eLabFTW entities. **Local is the source of truth.**

## Install

Python 3.10+, installed as a `uv` tool (provides the `elport` command):

```sh
uv tool install git+https://github.com/youthesame/elport
```

## The core idea

One local file maps to one eLabFTW entry: Markdown with a YAML front matter block, inline HTML allowed.

On `push`, elport uploads every real local file the body references, in any notation, and swaps each path for its
real eLabFTW URL. The body goes up as raw Markdown, so `<figure>` and `$...$` render as-is. To link another entry
instead of uploading, write its eLabFTW URL. Code fences, inline code, and HTML comments are never parsed.

## Quick start

```sh
elport login labA                       # store base_url (config) + api_key (OS keyring), interactively
elport list                             # what do I already have on the server? (read-only)
elport new "CRISPR titration"           # create the entity, scaffold report.md with id
# ...or start from an entity made in the Web UI: elport clone <id>
# ...edit report.md, drop fig1.png / data.csv next to it...
elport status                           # what would sync? (read-only)
elport push                             # upload references + push the body
```

## Commands

Commands that act on a local document take an optional `[<doc>]`, defaulting to `report.md`. Any name works.
Exit code `0` on success, `1` on failure. Run `elport <command> --help` for the full flags.

### Sync

| Command | Summary |
|---|---|
| `push` | Upload the body and its referenced files to one entity. |
| `pull` | Fetch the body and metadata, and download referenced files. |
| `fetch` | Download every attachment, referenced or not. Read-only. |
| `status` | Report what would sync, and in which mode. Read-only. |
| `diff` | Diff local against remote, or against the last push with `--base`. |
| `merge` | Merge `.base.md` and `.remote.md` into `<doc>` after a conflict. |

`push` creates the entity and writes `id` back to the front matter when `id` is unset, and it runs the mode and
conflict checks first. `merge` is local-only and shells out to `git merge-file`, with git itself optional.

### Entities

| Command | Summary |
|---|---|
| `new "<title>"` | Create an entity and scaffold front matter. |
| `clone <id>` | Start `<doc>` from an entity that already exists, then pull. |
| `list` | List remote entities as `id  date  title`. Read-only. |
| `view <id>` | Print one remote body to stdout, exactly as stored. |
| `comments` | Print the remote comment thread. |
| `comment "<text>"` | Post one comment. Use the Web UI to edit or delete. |

`new` and `clone` write to `report.md` unless you pass `-o <doc>`. `list` defaults to your own entities
(`--scope self`) and returns 50 at a time, with `-q <term>`, `--limit`, `--offset`, and `--json` to narrow it.
`comments` prints to the terminal only, never into the body.

### Setup

| Command | Summary |
|---|---|
| `login [<profile>]` | Store base_url in `config.toml`, api_key in the OS keyring. |
| `logout [<profile>]` | Remove the stored api_key. Keeps base_url. |
| `profile [use <name>]` | List profiles, or set the default one. |
| `whoami` | Check auth: user, team, role, key permissions, server version, scopes. |

### Shared options

- `-n/--dry-run` rehearses a push without sending.
- `-f/--force` pushes over a changed remote, losing the Web-side change.
- `-y/--yes` skips the two confirmations: widening `read`/`write` beyond your team, and new uploads over 25 MiB.
  A non-interactive run needs `-y` for either.
- `--profile <name>` and `--entity {experiments,items}` pick the destination.

### pull and fetch

Neither command is a superset of the other.

- `pull` writes referenced files back by **basename only**. A subdirectory path like `assets/fig.png` lands as
  `fig.png`.
- `pull` only downloads files the body links to. Attachments that sit on the entity without being embedded, like
  raw data or spectra, stay on the server.
- `fetch` brings those down. It never parses the body, touches the base, or feeds the push manifest, so a fetched
  file is not uploaded again unless you link it in the body yourself.
- If a local file differs, `fetch` leaves it in place and writes the remote copy beside it as `<name>.remote`.

## Document format (front matter)

A YAML block at the top, generated or completed on push if absent:

```markdown
---
id: 42                        # entity ID (auto-filled on creation)
entity: experiments           # experiments | items (default experiments)
title: "experiment title"     # optional; defaults to the file name on creation
tags: [CRISPR, PCR]           # optional; add-only (remove tags in the Web UI)
category: Molecular Biology   # optional; ID or existing category name (elport never creates categories)
status: Running               # optional; ID or existing status name (elport never creates statuses)
profile: labA                 # optional; destination profile
read: team                    # optional; owner | owner+admin | team | account | public
write: owner                  # optional; same scale
---

# Body. Markdown, inline HTML allowed ...
```

The block holds only `id`, human metadata, and an optional `profile`. The base and the hashes live in state. elport
strips the front matter before sending the body.

`title`, `category`, `status`, `read`, and `write` are reflected **only when present**. Omit a key and elport leaves
that remote value untouched. `pull` writes them back from the server, so a key you never wrote may appear after a
pull, while a key you edited locally but have not pushed is kept. `tags` are add-only on both sides, so pull keeps
the union.

`read`/`write` set the eLabFTW base visibility only, so individual Web-UI grants survive. Widening beyond your team
asks for confirmation.

If the front matter and the CLI disagree on profile, entity, or id, elport **stops** rather than guessing.

## Conflicts

Since you may also edit the body in the Web UI, `push` compares the current remote against the stored base first.

- **unchanged** proceeds.
- **changed** aborts. elport writes `<name>.base.md` (the ancestor) and `<name>.remote.md`. Run `elport merge` to
  3-way them into your file, resolve any `<<<<<<<`/`>>>>>>>` markers, then push. A body that still has markers is
  refused.
- **no base on this machine** aborts. Run `elport pull` first, or `--force` to overwrite blind.

`--force` discards the Web-side change, so use it deliberately. eLabFTW keeps server-side history recoverable from
the Web UI as a safety net, but elport keeps no local history of its own. Want per-edit history? `git init` your
notes folder, since the files are plain Markdown.

## Configuration & auth

**No credentials in the project.**

### Where the key lives

`elport login` stores the API key in your OS keyring: Keychain, Credential Manager, or Secret Service. Everything
that is not a secret, base_url included, goes in `~/.config/elport/config.toml` with mode `600`. elport never
prints the key.

```toml
# ~/.config/elport/config.toml
default_profile = "labA"

[profiles.labA]
base_url   = "https://lab-a.example.org"
verify_ssl = true
```

### Which credentials win

elport checks three sources, in order, and stops at the first hit.

1. `ELABFTW_BASE_URL` + `ELABFTW_API_KEY` in the environment. This is how CI authenticates.
2. The keyring and config pair from `elport login`. The normal default.
3. A plaintext key in the config. A last resort, used with a warning, and only when no keyring backend exists.

### Profiles

Profiles layer like settings.json, one per team:

```
~/.config/elport/config.toml  →  <project>/.elport.toml  →  <dir>/.elport.toml
```

Your first `elport login` becomes the default. `elport profile use <name>` switches it, or set `profile:` in a
note's front matter to pin that one file.

### .elportignore

`.elportignore` excludes referenced files, `.gitignore`-style, so they are never uploaded. It is additive across
the same layers as profiles, plus every `.elportignore` from the project root down to the document directory. Each
pattern is anchored at its own directory.

## Learn more

- **Why it works this way** → [docs/DESIGN.md](docs/DESIGN.md)
- **How eLabFTW's API actually behaves** → [docs/ELABFTW-API.md](docs/ELABFTW-API.md)
- **The behavioral contract** → the test suite (`tests/`) is authoritative
- **Driving elport from an AI agent** → [skills/elport/SKILL.md](skills/elport/SKILL.md)

## Development

| Module | Role |
|---|---|
| `cli.py` | Argument parsing and dispatch |
| `sync.py` | push, pull, status, diff |
| `transclude.py` | Path ↔ URL rewriting, both directions |
| `client.py` | eLabFTW API wrapper |
| `state.py` | The stored base and its hashes |
| `config.py` | Profiles and credential resolution |
| `frontmatter.py` | The YAML block |
| `browse.py` | list and view |

**The tests are the authoritative behavioral contract, so change tests first.** You can check live API behavior
against <https://demo.elabftw.net>.

## Acknowledgments

- [elab-doc-sync](https://github.com/Kosaku-Noba/elab-doc-sync)
- [elAPI](https://github.com/uhd-urz/elAPI)

## License

MIT
