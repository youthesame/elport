# elport: a git-like sync CLI for eLabFTW

[English](https://github.com/youthesame/elport/blob/main/README.md) | [日本語](https://github.com/youthesame/elport/blob/main/README_JA.md)

Write lab notes locally in Markdown, with inline HTML like `<figure>` where you want it. Then `push`/`pull` them,
along with every figure and data file the body references, to eLabFTW entities. **Local is the source of truth.**

## Install

Python 3.10+, installed as a `uv` tool (provides the `elport` command):

```sh
uv tool install elport    # upgrade: uv tool upgrade elport
```

Or with pip:

```sh
pip install elport        # upgrade: pip install -U elport
```

## The core idea

One local file maps to one eLabFTW entry: Markdown with a YAML front matter block, inline HTML allowed.

On `push`, elport uploads every real local file the body references, in any notation and anywhere on disk
(`../data/raw.csv` and absolute paths work; hidden files never go up), and swaps each path for its real eLabFTW
URL. The body goes up as raw Markdown, so `<figure>` and `$...$` render as-is. To link another entry
instead of uploading, write its eLabFTW URL or its local document's path. Code fences, inline code, and HTML comments are never parsed.

## Quick start

```sh
elport login labA                       # store base_url (config) + api_key (OS keyring), interactively
elport list                             # what do I already have on the server? (read-only)
elport new "CRISPR titration"           # create the entity, scaffold report.md with id
# ...or edit an entity made in the Web UI: elport clone <id> (to only read it: elport view <id>)
# ...edit report.md, drop fig1.png / data.csv next to it...
elport status                           # what would sync? (read-only)
elport push                             # upload references + push the body
```

## Writing a note

```markdown
---
id: 42                    # written by elport new or the first push
title: CRISPR titration
tags: [CRISPR]
---

# Result

![titration curve](fig1.png)

Raw data: [raw.csv](data/raw.csv)
```

`elport new` writes the front matter for you. Write the body as usual and reference local files by their path, and
push uploads them. `elport help documents` lists every front matter key and reference rule.

## Commands

Commands that act on a local document take an optional `[<doc>]`, defaulting to `report.md`. Any name works. Sync commands
also take a directory: every elport document under it, one line each.
Exit code `0` on success, `1` on failure. Run `elport <command> --help` for the full flags.

### Sync

| Command | Summary |
|---|---|
| `push` | Upload the body and its referenced files to one entity. |
| `pull` | Fetch the body and metadata, and download referenced files. |
| `fetch` | Download every attachment, referenced or not. Read-only. |
| `status` | Report what would sync, and in which mode. Read-only. |
| `diff` | Diff local against remote, or against the last push with `--base`. |
| `merge` | Merge `.base.md` and `.remote.md` into `<doc>` after a conflict. Requires `git`. |

`push` creates the entity and writes `id` back to the front matter when `id` is unset, and it runs the mode and
conflict checks first. `merge` is local-only: it runs `git merge-file`, so `git` must be installed, but the folder
need not be a git repository.

### Entities

| Command | Summary |
|---|---|
| `new "<title>"` | Create an entity and scaffold front matter. |
| `clone <id>` | Start `<doc>` to edit and push an entity that already exists, then pull. To only read one, use `view`. |
| `clone --all <dir>` | Back up every entity into `<dir>`. Re-run to add new ones. An HTML entity is saved as `view -o` writes it. |
| `list` | List remote entities as `id  date  title`. Read-only. |
| `view <id>` | Read one remote entity: print its body to stdout, made readable. Read-only. An HTML entity loses its styling, and attachments appear by file name. |
| `view <id> -o <dir>` | Read it with the images and files it shows, saved into a new or empty `<dir>`. Not a working copy. |
| `activity` | Heatmap of the entities you created per day, up to a year as the terminal width allows. `--team` gives one per author. |
| `comments [<id>]` | Print the remote comment thread. With an `<id>`, no local document is needed. |
| `comment "<text>"` | Post one comment. Use the Web UI to edit or delete. |

`new` and `clone` write to `report.md` unless you pass `-o <doc>`. `list` defaults to your own entities
(`--scope self`) and returns 50 at a time, with `-q <term>`, `--limit`, `--offset`, and `--json` to narrow it;
`--all` pages through every result.
`comments` prints to the terminal only, never into the body.

An entity written in the Web UI's rich text editor stores HTML. elport only reads it: `clone`, `pull`, and `push`
refuse it, and `view -o` is the way to read it with its figures.

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

## Documents, sync, and conflicts

The details ship with the CLI, so they match the version you installed:

- `elport help documents`: front matter, references, attachments, links between notes ([on GitHub](https://github.com/youthesame/elport/blob/main/src/elport/help/documents.md))
- `elport help workflow`: the status, diff, push loop, conflicts, pull and fetch ([on GitHub](https://github.com/youthesame/elport/blob/main/src/elport/help/workflow.md))
- `elport help agents`: what an AI agent asks its user before acting ([on GitHub](https://github.com/youthesame/elport/blob/main/src/elport/help/agents.md))

## Configuration & auth

**No credentials in the project.**

### Where the key lives

`elport login` stores the API key in your OS keyring: Keychain, Credential Manager, or Secret Service. Everything
that is not a secret, base_url included, goes in `~/.config/elport/config.toml` with mode `600`. elport never
prints the key, and refuses a base_url that is not `https://`.

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
3. A plaintext key in `~/.config/elport/config.toml`. A last resort, used with a warning, and only when no keyring backend exists (e.g. WSL, headless servers).

### Profiles

Profiles layer like settings.json, one per team:

```
~/.config/elport/config.toml  →  <project>/.elport.toml  →  <dir>/.elport.toml
```

Your first `elport login` becomes the default. `elport profile use <name>` switches it, or set `profile:` in a
note's front matter to pin that one file.

### .elportignore

`.elportignore` excludes referenced files inside the document directory, `.gitignore`-style, so they are never
uploaded. It is additive across
the same layers as profiles, plus every `.elportignore` from the project root down to the document directory. Each
pattern is anchored at its own directory.

## Learn more

- **Why it works this way** → [docs/DESIGN.md](https://github.com/youthesame/elport/blob/main/docs/DESIGN.md)
- **How eLabFTW's API actually behaves** → [docs/ELABFTW-API.md](https://github.com/youthesame/elport/blob/main/docs/ELABFTW-API.md)
- **The behavioral contract** → the test suite (`tests/`) is authoritative
- **Driving elport from an AI agent** → `elport help agents`. The skill in [skills/elport/SKILL.md](https://github.com/youthesame/elport/blob/main/skills/elport/SKILL.md) only points there.

## Development

| Module | Role |
|---|---|
| `cli.py` | Argument parsing and dispatch |
| `sync.py` | push, pull, status, diff, view |
| `transclude.py` | Path ↔ URL rewriting, both directions |
| `client.py` | eLabFTW API wrapper |
| `state.py` | The stored base and its hashes |
| `config.py` | Profiles and credential resolution |
| `frontmatter.py` | The YAML block |
| `browse.py` | list and activity |
| `help/*.md` | Topics printed by `elport help` |

**The tests are the authoritative behavioral contract, so change tests first.** You can check live API behavior
against <https://demo.elabftw.net>.

## Acknowledgments

- [elab-doc-sync](https://github.com/Kosaku-Noba/elab-doc-sync)
- [elAPI](https://github.com/uhd-urz/elAPI)

## License

MIT
