# elport: a git-like sync CLI for eLabFTW

[English](https://github.com/youthesame/elport/blob/main/README.md) | [日本語](https://github.com/youthesame/elport/blob/main/README_JA.md)

Write lab documents locally in Markdown (with inline HTML like `<figure>` on eLabFTW 5.x). Then `push`/`pull` them,
along with every figure and data file the body references, to eLabFTW entities. **Local is the source of truth.**

## Install

**macOS / Linux:**

```sh
brew install youthesame/tap/elport
```

Without Homebrew:

```sh
curl -fsSL https://raw.githubusercontent.com/youthesame/elport/main/install.sh | sh
```

**Windows (PowerShell):**

```powershell
irm -UseBasicParsing https://raw.githubusercontent.com/youthesame/elport/main/install.ps1 | iex
```

<details><summary>Other ways (Go, GitHub Releases)</summary>

- Go 1.26+: `go install github.com/youthesame/elport/cmd/elport@latest`
- [GitHub Releases](https://github.com/youthesame/elport/releases) (on macOS, browser downloads are quarantined; prefer Homebrew or `install.sh`)

</details>

### Upgrading from Python `v0.1.x`

Existing `~/.config/elport/config.toml` and `~/.config/elport/state/` work as-is on all OSes (do not delete them).

1. Uninstall the Python package first (`uv tool uninstall elport`, `pipx uninstall elport`, `pip uninstall elport`, or your conda environment) so it does not shadow the Go binary on `PATH`.
2. Install the Go binary above, then check `command -v elport` (PowerShell: `Get-Command elport`) and `elport --version` (`0.2.x`).

- **macOS**: on first read of a Keychain entry created by Python `v0.1.x`, macOS prompts once for Keychain access
  (`/usr/bin/security`); click **Always Allow** or re-run `elport login <profile>` once.
- **Windows / KDE KWallet**: run `elport login <profile>` once after upgrading (native keyring storage format differs).
- **Python `v0.1.x` line**: preserved on the [`python` branch](https://github.com/youthesame/elport/tree/python) and PyPI.

## The core idea

One local file maps to one eLabFTW entity: Markdown with a YAML front matter block.

On `push`, elport uploads every real local file the body references, in any notation and anywhere on disk
(`../data/raw.csv` and absolute paths work; hidden files never go up), and swaps each path for its real eLabFTW
URL. The body goes up as raw Markdown, so `$...$` renders as-is; inline HTML such as `<figure>` renders on eLabFTW
5.x, whereas eLabFTW 6.x strips raw HTML from rendered Markdown (use `![alt](fig.png)` on 6.x). To link another
entity instead of uploading, write its eLabFTW URL or its local document's path. Code fences, inline code, and HTML
comments are never parsed.

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

## Writing a document

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
Exit code `0` on success, `1` on failure, `2` on usage error. Run `elport <command> --help` for the full flags.

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
| `list` | List remote entities as `id  date  title`. Read-only. `--order lastchange` or `--order comment` shows that time instead of the date. |
| `view <id>` | Read one remote entity: print its body to stdout, made readable. Read-only. An HTML entity loses its styling, and attachments appear by file name. Links to and from it are listed on stderr. |
| `view <id> -o <dir>` | Read it with all its attachments, saved into a new or empty `<dir>`. Not a working copy. |
| `activity` | Heatmap of the entities you created per day, up to a year as the terminal width allows. `--team` gives one per author. |
| `comments [<id>]` | Print the remote comment thread. With an `<id>`, no local document is needed. |
| `comment "<text>"` | Post one comment. Use the Web UI to edit or delete. |

`new` and `clone` write to `report.md` unless you pass `-o <doc>`. `list` defaults to your own entities
(`--scope self`) and returns 50 at a time, with `-q <term>`, `--tag <tag>`, `--limit`, `--offset`, and `--json` to
narrow it; `--all` pages through every result. `-q` also takes the Web UI's search fields, such as
`-q 'author:"Jane Doe" status:"Success"'`. `--order` and `--sort asc|desc` pick the order, as the Web UI's Sort menu
does: `elport list --scope team --order comment` puts the team's latest comments first.
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

The details ship embedded in the CLI, so they match the version you installed:

- `elport help documents`: front matter, references, attachments, links between documents ([on GitHub](https://github.com/youthesame/elport/blob/main/internal/help/documents.md))
- `elport help workflow`: the status, diff, push loop, conflicts, pull and fetch ([on GitHub](https://github.com/youthesame/elport/blob/main/internal/help/workflow.md))
- `elport help agents`: what an AI agent asks its user before acting ([on GitHub](https://github.com/youthesame/elport/blob/main/internal/help/agents.md))

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
document's front matter to pin that one file.

### .elportignore

`.elportignore` excludes referenced files inside the document directory, `.gitignore`-style, so they are never
uploaded. It is additive across
the same layers as profiles, plus every `.elportignore` from the project root down to the document directory. Each
pattern is anchored at its own directory.

## Learn more

- **How eLabFTW's API actually behaves** → [docs/ELABFTW-API.md](https://github.com/youthesame/elport/blob/main/docs/ELABFTW-API.md)
- **The behavioral contract** → Go tests (`internal/**/*_test.go`), `testdata/golden/`, and Quint traces (`spec/quint/sync.qnt` → `testdata/quint/`)
- **Driving elport from an AI agent** → `elport help agents`. The skill in [skills/elport/SKILL.md](https://github.com/youthesame/elport/blob/main/skills/elport/SKILL.md) points there.
- **Historical Python `v0.1.x` design notes & ADRs** → preserved on the [`python` branch](https://github.com/youthesame/elport/tree/python)

## Development

| Package | Role |
|---|---|
| `cmd/elport` | CLI entry point and signal handling |
| `internal/cli` | Flag parsing, dispatch, `new`, `clone`, batch runs |
| `internal/sync` | `push`, `pull`, `merge`, `fetch`, `status`, `diff`, `view`, `comments` |
| `internal/plan` | Upload approval boundary (`ApprovedUploadPath`) |
| `internal/transclude` | Hand-written reference scanner and path ↔ URL rewriting |
| `internal/frontmatter` | YAML 1.1-compatible front matter parsing and rendering |
| `internal/client` | HTTPS eLabFTW REST v2 client with idle timeouts and origin guards |
| `internal/browse` | `list` and `activity` heatmap |
| `internal/state` | Stored base state and key hashes |
| `internal/config` | Layered config, profiles, and redacted `APIKey` resolution |
| `internal/fsx` | Confined `*os.Root` writes and atomic replacement |
| `internal/help` | Embedded Markdown help topics (`elport help`) |
| `arch_test.go` | Structural guardrails and `AGENTS.md` symbol checks |

**The tests are the authoritative behavioral contract, so change tests first.** Run `go test -race ./...` and
`go vet ./...`. You can check live API behavior against <https://demo.elabftw.net>.

## Acknowledgments

- [elab-doc-sync](https://github.com/Kosaku-Noba/elab-doc-sync)
- [elAPI](https://github.com/uhd-urz/elAPI)

## License

MIT
