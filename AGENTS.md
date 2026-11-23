# elport: project guide for AI agents

`elport` is a **git-like sync CLI for eLabFTW** built as a single static Go binary (`CGO_ENABLED=0`).
**Local is the source of truth.**

## Documentation map

- **Behavior = the tests**: Go unit/property/fuzz tests (`internal/**/*_test.go`), golden fixtures
  (`testdata/golden/`), and Quint ITF protocol traces (`spec/quint/sync.qnt` → `testdata/quint/`).
  Update tests first when changing behavior.
- **Architectural rules = `arch_test.go`**: mechanically enforces no production comments (only
  compiler directives like `//go:embed`), package import boundaries, banned APIs (`os.UserConfigDir`,
  `XDG_CONFIG_HOME`, `http.Client.Timeout`, `http.MethodDelete`, `regexp` in `transclude`,
  `os/exec` outside `internal/sync`, `os.Exit` outside `cmd/elport/main.go`), and that every Go
  symbol referenced below exists.
- **Measured eLabFTW API facts = [docs/ELABFTW-API.md](docs/ELABFTW-API.md)**: read before changing
  requests, URLs, or body/metadata handling. Update only when the demo's version changes.
- **User-facing usage = [README.md](README.md)** and embedded help topics in
  [internal/help/](internal/help/) (`elport help <topic>`). Keep entries concise.
- **Driving elport from an agent = `elport help agents`** ([internal/help/agents.md](internal/help/agents.md)).
  [skills/elport/SKILL.md](skills/elport/SKILL.md) points there.
- **Historical Python `v0.1.x` & retired ADRs**: preserved on the `python` branch. Stage 2
  (opt-in one-way HTML publish for eLabFTW 6.x) is tracked in Issue #112.

Keep features **thin** (YAGNI): single engine, markdown-native, explicit-and-simple, secret isolation.

## Terminology (use these words in code, messages, tests, and docs)

| Term | Meaning | Avoid |
|---|---|---|
| **Document** | Local Markdown file with front matter mapping to one entity | note, page, file |
| **Entity** | Remote eLabFTW experiment or resource | entry, record |
| **HTML entity** | Entity with HTML body (`content_type: 1`); read-only | rich-text entity |
| **Reference** | Local path written in the body that `push` uploads & rewrites | embed, link (for files) |
| **Remote-linked path** | Bare local path seen in a remote body; never uploaded unless listed | foreign link |
| **Document link** | Reference to another document, sent as its entity URL (never uploaded) | — |
| **Attachment** | File stored on an entity (eLabFTW "upload") | upload (noun), file |
| **Listed attachment** | File named under front matter `attachments:` | unlinked attachment, extra file |
| **Base** | Last-synced state in two forms (`local_base` and `remote_base`) | — |
| **Working copy** | The one document holding an entity's base on this machine | checkout, clone (noun) |
| **Snapshot** | Readable body + all attachments (`view -o`); no front matter, no base | export, archive |

## Invariants (enforced in code and tests)

- **Local is authoritative.** `internal/sync.Push` overwrites the remote body only after conflict
  detection, rewrites references only on the outgoing copy, and never mutates the local body.
- **Two-form base, same-form compare only:** `internal/sync.LocalBody` and `internal/sync.RemoteBody`
  are distinct named types; `remote_base` comes from the push `PATCH` response, not sent bytes.
- **Nothing out of sync goes into the body:** comments (`internal/sync.ShowComments`), links, and
  steps print to the terminal only.
- **Explicit upload approval boundary:** only `internal/plan.ApproveFromLocal` / `internal/plan.Approve`
  mint `internal/plan.ApprovedUploadPath` (after `internal/transclude.ExposesHidden`, ignore, listed
  attachments, and remote-linked path exclusion). `internal/client.Client.Upload` accepts only
  `internal/plan.ApprovedUploadPath` and opens via `internal/plan.ApprovedUploadPath.Open`
  (`*os.Root` + `Lstat` + `os.SameFile`).
- **Never parse inside code fences, inline code, or HTML comments** (`internal/transclude.Extract`).
- **Document-directory writes are confined:** `internal/fsx.RootDir` (`*os.Root`, `Lstat` symlink
  rejection, `O_EXCL`/no-clobber) governs `internal/sync.Pull`, `internal/sync.Fetch`,
  `internal/sync.Snapshot`, and `internal/sync.Merge`.
- **Attachment reuse by SHA-256 only; never delete attachments:** `internal/sync.MatchingUpload`
  requires a matching server-returned SHA-256; `arch_test.go` bans HTTP `DELETE` in `internal/client`.
- **Locked and non-empty HTML entities are refused:** `internal/sync.Push` refuses locked entities
  even with `--force` and re-checks before `PATCH`; `internal/sync.RefuseHTML` blocks non-empty HTML
  entities in `push`, `pull`, and `clone`.
- **Secret isolation:** `internal/config.APIKey` redacts all formatting; config/state live under
  `<home>/.config/elport/` on all OSes (mode `0600` on Unix).

## Testing and verification

- Offline suite: `go test -race ./...`, `go vet ./...`, `gofmt -l .`, `go test -v -run TestArch .`,
  and `CGO_ENABLED=0 go build ./cmd/elport`.
- Quint model (optional regeneration when changing sync protocol):
  `npx @informalsystems/quint run --max-samples=2000 --max-steps=20 --invariant=allSafetyInvariants spec/quint/sync.qnt`
  and re-export ITF JSON vectors to `testdata/quint/`.
- Live API checks: use **only the public disposable demo <https://demo.elabftw.net>**. Never write
  to any other instance.
