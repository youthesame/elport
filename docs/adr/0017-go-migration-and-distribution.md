---
status: proposed
---

# Migrate elport to a single-binary Go implementation

elport is intended for both AI-driven workflows and managed wet-lab PCs (Windows, macOS, and
shared Linux compute hosts) where requiring Python or `uv` is an adoption barrier. Excluding
migration cost, a static pure-Go (`CGO_ENABLED=0`) binary fits elport's thin-CLI philosophy best.
We therefore migrate the full current feature set (including 5.x Markdown `clone`/`pull`/`merge`)
to Go **before** implementing [ADR-0016](0016-one-way-html-publish-for-elabftw-6.md), freezing
Python semantics during the port so language migration and HTML publish never overlap and HTML
rendering (`goldmark`) is written only once.

- **Same repository, `python` maintenance branch, `0.x` versioning.** Keep the `elport` name,
  history, issues, and ADRs in this repository. Pin the baseline Python commit and `uv.lock` on a
  `python` branch. Stay on `0.x` (`v0.2.0-alpha.*` pre-releases, not `v2.0.0`, which would force a
  `/v2` Go module path suffix and break `go install ...@latest`). Split `.github/workflows/release.yml`
  so Go prerelease tags never trigger the existing `v*` PyPI Python publish workflow. Trial Go
  through prereleases before switching `main`; `pytest` remains authoritative until cutover, after
  which Go tests become authoritative and `AGENTS.md` is updated. Automatic `merge` still requires
  `git`, as in the Python CLI.
- **Practical compatibility with existing state and credentials.**
  - Keep `<home>/.config/elport/config.toml` and `<home>/.config/elport/state/` on **all** OSes
    (macOS, Linux, and Windows; never `os.UserConfigDir()` and never `XDG_CONFIG_HOME`), preserving
    the exact state key hash `sha256("{base_url_without_trailing_slash}|{entity}|{id}")` and all
    saved fields (including interrupted creation and unresolved conflicts).
  - Keyring compatibility only where free: test native lookup on each OS with the pinned backend.
    macOS Keychain and Linux Secret Service are expected to read existing entries (watch for
    first-read macOS ACL prompts and `go-keyring-base64:` rollback incompatibility); Windows
    WinVault (`TargetName=elport`/`<profile>@elport`, UTF-16LE) and KDE KWallet are not
    transparently compatible, so document a one-time `elport login` on upgrade rather than adding
    compatibility shims. Preserve the existing distinction between missing credentials
    (`ErrNotFound` → no fallback) and unavailable backend errors (→ user TOML fallback).
  - Normalize CRLF and lone CR to LF on document text reads (matching Python universal newlines)
    without mutating attachment bytes or stored remote-base strings.
- **Minimal Go stack and known porting traps.** Prefer standard library (`net/http`,
  `mime/multipart`, `crypto/sha256`, `embed` for `help/*.md`, `go test -fuzz`) plus minimal
  dependencies (`cobra` or `urfave/cli` v3 chosen via representative CLI fixture; `gopkg.in/yaml.v3`;
  `BurntSushi/toml`; `zalando/go-keyring`; `golang.org/x/term`; `golang.org/x/net/html`;
  `golang.org/x/text` for NFC/casefold; a unified diff library; `rapid` for property tests; and
  `goldmark` for ADR-0016). Account explicitly for verified porting traps:
  - **RE2 and Unicode:** seven regexes in `transclude.py` fail Go RE2 compilation (lookaround,
    `\1`, `\Z`); replace fence/code-span/raw-text/comment/tag matching with hand-written scanners.
    Go RE2 `\s`/`\w` are ASCII-only (unlike Python's Unicode `\s`/`\w`, affecting Japanese text),
    and spans use UTF-8 byte offsets rather than code points; `path.Match` differs from Python
    `fnmatchcase` (`[!a]`).
  - **Filesystem and paths:** `filepath.EvalSymlinks` errors on non-existent paths (unlike
    non-strict `Path.resolve()`), directly affecting dotfile resolution checks; test atomic file
    replacement and permissions on Windows.
  - **YAML front matter:** reject duplicate keys and aliases; decide YAML 1.1 (`yes`/`on`/`017`)
    handling explicitly; hand-write front matter emission for the fixed keys and golden-test it
    against PyYAML so cutover does not reformat every document.
  - **CLI and TLS:** verify trailing flags (`elport push doc.md --force`), `--`, and exit codes
    `0`/`1`/`2` (document dropping argparse `--forc` prefix abbreviations). Use the OS trust store
    (changed from Python's `certifi`; honor explicit CA bundle env vars if retained), restrict
    redirected `Authorization` headers to the same HTTPS origin, and split dial/header timeouts
    instead of total `http.Client.Timeout` so large attachment transfers do not time out.
- **Verification: golden fixtures plus stateful HTTP differential testing.** Export
  language-independent golden fixtures from the pinned Python CLI + Hypothesis for `transclude` and
  `frontmatter`, and run black-box differential tests driving both the pinned Python binary and the
  Go binary against a stateful fake HTTP eLabFTW server (comparing exit codes, written documents,
  state, and HTTP method/path/order/payload/attachment bytes, rather than help prose formatting).
- **Focused distribution via GitHub Actions + GoReleaser.**
  - **Initial channels:** GitHub Releases (`CGO_ENABLED=0` archives + SHA-256 checksums for
    macOS/Windows/Linux), Homebrew tap (macOS + Linuxbrew), `install.sh` (Linux `~/.local/bin`),
    `install.ps1` (Windows `%LOCALAPPDATA%\elport`), and a clear PyPI transition (either keep
    existing Python releases with documented switch/rollback, or publish Go binary wheels). Verify
    whether unsigned binaries run cleanly on target managed Windows PCs (AppLocker/SmartScreen)
    before relying on `%LOCALAPPDATA%` delivery.
  - **Add on demand / at stable:** `winget` (portable `.zip`), Nix (`flake.nix` / NUR), Scoop,
    `.deb`/`.rpm`/`.apk` (`nfpms`), AUR, and separate nightly channels.

## Considered options

- **Implement ADR-0016 in Python first, then port to Go.** Delivers 6.x HTML publish sooner and
  keeps Hypothesis/CrossHair during design, but requires implementing and eLabFTW-verifying the
  Markdown-to-HTML renderer twice and adds HTML-entity base compatibility to the migration.
- **Stay in Python permanently.** Zero migration cost, but retains Python/`uv` setup friction on
  managed wet-lab PCs.
- **Migrate to Rust.** Strongest compile-time exhaustiveness (`enum`/`Result`), `clap`, and
  `proptest`/`Kani`, but slower compile/CI loops and heavier cross-compilation for an HTTP + text
  CLI.
- **Use Quint / Lean 4 to verify the Go port itself.** Porting bugs cluster in parsing,
  regex/Unicode, YAML, OS paths, and keyring rather than abstract decision logic; formal protocol
  modeling is reserved for ADR-0016's sync state machine instead.
