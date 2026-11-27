# elport

[English](README.md) | [日本語](README_JA.md)

A command-line tool for eLabFTW, built for both researchers and AI agents.
Read and search your lab's entities from the terminal, or write documents locally in Markdown and push them with their figures and data. Your local files are always the master copy.

- **Read and search**: browse and search entities, read bodies, revisions, comments, and attachments, or save local copies.
- **Write and sync**: one local Markdown document maps to one eLabFTW entity; `push` uploads referenced files and checks for conflicts first.
- **Safe by default**: never deletes anything, offers a local read-only mode, and sends no telemetry.

## Install

**macOS / Linux (Homebrew):**

```sh
brew install youthesame/tap/elport
```

**macOS / Linux (shell script):**

```sh
curl -fsSL https://raw.githubusercontent.com/youthesame/elport/main/install.sh | sh
```

**Windows (PowerShell):**

```powershell
irm -UseBasicParsing https://raw.githubusercontent.com/youthesame/elport/main/install.ps1 | iex
```

<details><summary>Other ways (Nix, Go)</summary>

**Nix** (requires `nix-command` and `flakes`):

```sh
nix profile install github:youthesame/elport
```

Or run without installing:

```sh
nix run github:youthesame/elport -- --help
```

**Go 1.26+:**

```sh
go install github.com/youthesame/elport/cmd/elport@latest
```

</details>

<details><summary>Upgrading from Python v0.1.x</summary>

Existing `~/.config/elport/config.toml` and `~/.config/elport/state/` work as-is on all OSes. Do not delete them.

1. Uninstall the Python package first (`uv tool uninstall elport`, `conda global uninstall elport`, `pipx uninstall elport`, or `pip uninstall elport`) so it does not shadow the Go binary on `PATH`.
2. Install the Go binary above, then check `command -v elport` (PowerShell: `Get-Command elport`) and `elport --version`.

- **macOS**: on first read of a Keychain entry created by Python `v0.1.x`, macOS prompts once for `/usr/bin/security` access; click **Always Allow** or re-run `elport login <profile>` once.
- **Windows / Linux (KDE)**: run `elport login <profile>` once after upgrading because native keyring storage differs.
- **Python `v0.1.x` line**: preserved on the [`python` branch](https://github.com/youthesame/elport/tree/python) and PyPI.

</details>

## Quick start

### Read and search

```sh
elport login labA                       # save base_url in config and api_key in OS keyring
elport list --scope team -q "CRISPR"    # search team entities
elport view 42                          # read an entity body
elport revisions 42                     # list saved body revisions
```

### Write and sync

```sh
elport new "CRISPR titration"           # create an entity and scaffold report.md
# ...edit report.md and place fig1.png or data/raw.csv nearby...
elport status                           # check what would sync
elport push                             # upload referenced files and push the body
```

A document is Markdown with YAML front matter:

```markdown
---
id: 42
title: CRISPR titration
tags: [CRISPR]
---

# Result

![titration curve](fig1.png)

Raw data: [raw.csv](data/raw.csv)
```

On `push`, elport uploads every referenced local file and rewrites paths to eLabFTW URLs on the server without changing your local file. Run `elport help documents` for full reference and entity-link rules.

<details><summary>Front matter keys</summary>

| Key | Value / type | Default / behavior |
|---|---|---|
| `id` | Positive integer | Assigned by `new` or the first `push`; do not hand-edit. |
| `entity` | `experiments` \| `items` | Defaults to `experiments`. |
| `format` | `markdown` \| `html` | Defaults to `markdown`; `html` is one-way publish (see below). |
| `title` | String | Defaults to the file name on creation. |
| `tags` | List (or single value) | Add-only; `pull` merges the union of local and remote tags (remove in the Web UI). |
| `category` | ID or existing name | elport never creates a new category. |
| `status` | ID or existing name | elport never creates a new status. |
| `profile` | Profile name | Pins the configured server for this document. |
| `read` | `owner` \| `owner+admin` \| `team` \| `account` \| `public` | Base visibility only (Web UI grants stay); widening beyond `team` prompts. |
| `write` | Same 5 levels as `read` | Same rules as `read`. |
| `attachments` | List of local paths | Files to upload as attachments without referencing them in the body. |

- **Sync rules**: `push` sends only keys edited since the last sync, so untouched or removed keys keep their remote values. Unknown keys are rejected.
- **`format: markdown` (default)**: sends raw Markdown with two-way `push`/`pull`/`merge`. eLabFTW 5.x renders inline HTML such as `<figure>` in Markdown bodies, whereas eLabFTW 6.x strips raw HTML from Markdown bodies.
- **`format: html` (one-way publish)**: create with `elport new --format html "<title>"` (or add to an existing synced Markdown document). `push` renders Markdown to HTML while preserving `<figure>`, `<img width>`, `<sub>`/`<sup>`, `<s>`, table `<br>`, and `$...$` math. `pull` updates front matter only; if edited in the Web UI, review with `elport diff` and run `elport push --confirm-remote <hash>`. Set `format: markdown` explicitly to revert.

</details>

## Using with AI agents

elport works directly with AI agents as a single static binary, with no MCP server or extra runtime.

- **Built-in guide**: run `elport help agents`, or install [skills/elport/SKILL.md](skills/elport/SKILL.md) so the agent always reads the guide matching your installed version.
- **Read-only mode**: set `ELPORT_READ_ONLY=1` or `read_only = true` in config to block `new`, `comment`, and live `push` locally.
- **Clean output**: `--json`, `revisions --rev`, and `view --attachment` write raw bytes when piped, while terminal output escapes control characters.

## Commands

Document commands default to `report.md`, and sync commands also accept a directory. Exit codes: `0` success, `1` failure, `2` usage error. Run `elport <command> --help` for full flags.

### Read and inspect

| Command | Summary |
|---|---|
| `list` | List or search entities (`-q`, `--tag`, `--scope`, `--order`, `--all`, `--json`). |
| `view <id>` | Print an entity body, save a snapshot (`-o <dir>`), print raw JSON (`--json`), or stream an attachment (`--attachment <name>`). |
| `revisions [<doc-or-id>]` | List body revisions, or print one verbatim with `--rev <id>`. |
| `comments [<id>]` | Print remote comments to the terminal. |
| `activity` | Show a daily creation heatmap (`--team` groups by author). |
| `fetch` | Download all attachments of a document without changing the body. |

### Write and sync

| Command | Summary |
|---|---|
| `new "<title>"` | Create an entity and scaffold a local document. |
| `clone <id>` | Create a local working copy of an entity (`--all <dir>` mirrors all). |
| `status` | Show what would sync and in which mode. |
| `diff` | Compare local against remote, or against the last sync with `--base`. |
| `push` | Check conflicts, upload referenced files, and push the body. |
| `pull` | Fetch remote changes and referenced attachments. |
| `merge` | Three-way merge after a conflict using `git`. |
| `comment "<text>"` | Post a comment to an entity. |

### Setup

| Command | Summary |
|---|---|
| `login [<profile>]` | Save `base_url` in `config.toml` and `api_key` in the OS keyring. |
| `logout [<profile>]` | Remove the stored `api_key` and keep `base_url`. |
| `profile [use <name>]` | List profiles or switch the default. |
| `whoami` | Show auth details, server version, and read-only state. |

**Shared options**: `-n`/`--dry-run` previews a push; `-f`/`--force` overwrites remote changes; `-y`/`--yes` skips confirmation prompts; `--profile <name>` and `--entity {experiments,items}` choose the target.

## Security, auth, and config

**No credentials in the project. No telemetry.** The elport CLI sends eLabFTW requests only to the HTTPS origin of your configured `base_url` and refuses cross-origin redirects. The CLI sends no telemetry, collects no usage analytics, and performs no update checks.

- **Credentials**: API keys live in the OS keyring and non-secret settings in `~/.config/elport/config.toml` (mode `600`). Order of precedence: `ELABFTW_BASE_URL` + `ELABFTW_API_KEY` env vars, OS keyring, then plaintext config fallback when no keyring exists.
- **Profiles**: layered from `~/.config/elport/config.toml` to `<project>/.elport.toml` to `<dir>/.elport.toml`, or pinned with `profile:` in front matter.
- **Read-only mode**: `ELPORT_READ_ONLY=1` or top-level `read_only = true` in any config layer blocks remote writes and cannot be disabled by a lower layer. For server-side enforcement, use a read-only eLabFTW API key.
- **`.elportignore`**: `.gitignore`-style rules excluding files in a document directory from upload.

## Documentation and development

Detailed references ship inside the binary so they always match your installed version:

- `elport help documents`: front matter, references, attachments, `format: html`, and entity links ([source](internal/help/documents.md))
- `elport help workflow`: sync loop, conflicts, `pull`, and `fetch` ([source](internal/help/workflow.md))
- `elport help agents`: agent workflows, confirmation rules, and untrusted remote content ([source](internal/help/agents.md))

Repository docs:

- [docs/ELABFTW-API.md](docs/ELABFTW-API.md): measured eLabFTW REST API behavior
- [AGENTS.md](AGENTS.md): invariants, architecture rules, and checks (`go vet ./... && go test -race ./...`; live checks only on <https://demo.elabftw.net>)
- [`python` branch](https://github.com/youthesame/elport/tree/python): legacy Python `v0.1.x` line and historical ADRs

## Acknowledgments

- [elab-doc-sync](https://github.com/Kosaku-Noba/elab-doc-sync)
- [elAPI](https://github.com/uhd-urz/elAPI)

## License

MIT
