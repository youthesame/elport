# elport — agent guide

Git-like sync CLI for eLabFTW (single static Go binary, `CGO_ENABLED=0`). **Local is the source of truth.**

## Rules you cannot infer from code alone

- **Tests are authoritative**: update tests first (`internal/**/*_test.go`, `testdata/golden/`, `spec/quint/sync.qnt` → `testdata/quint/`).
- **Read [docs/ELABFTW-API.md](docs/ELABFTW-API.md)** before touching HTTP requests, URLs, or body/metadata handling — mocks do not show real eLabFTW quirks.
- **Live API checks**: use **only** the disposable demo <https://demo.elabftw.net>. Never write to any other instance.
- **Terminology** (use these exact words; avoid the alternatives):
  - **Document** (not note/page/file), **Entity** (not entry/record), **HTML entity** (not rich-text entity)
  - **Reference** (not embed/link), **Remote-linked path** (not foreign link), **Attachment** (not upload/file)
  - **Working copy** (not checkout/clone), **Snapshot** (not export/archive)
- **Key safety boundaries** (enforced by types and `arch_test.go`): `internal/plan.ApprovedUploadPath`, `internal/sync.LocalBody`, `internal/sync.RemoteBody`, `internal/fsx.RootDir`, `internal/config.APIKey`.
- **Other pointers**: embedded CLI help is in [internal/help/](internal/help/) (`elport help agents`); Stage 2 HTML publish is Issue #112; legacy Python `v0.1.x` & ADRs are on the `python` branch.

## Verify

```sh
test -z "$(gofmt -l .)" && go vet ./... && go test -race ./... && CGO_ENABLED=0 go build ./cmd/elport
```
