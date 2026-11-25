package sync_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	gosync "sync"
	"testing"

	"github.com/youthesame/elport/internal/state"
	"github.com/youthesame/elport/internal/sync"
)

func setupHTMLDoc(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "report.md")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestHTMLNewPublishRepushAndNoOpAttachmentCheck(t *testing.T) {
	// Verifies first HTML publish, math-masked reference planning ($x_{[1]}(t)$ does not
	// upload local file "t"), <s> strikethrough & \$ preservation, 6.x warning suppression,
	// one-time first-publish stderr note, unchanged PATCH skip, and re-PATCH when a local
	// file switches to a different already-uploaded server attachment (same filename, new SHA-256).
	setIsolatedHome(t)
	dir := t.TempDir()
	figA := []byte("fig-a-bytes")
	figB := []byte("fig-b-bytes")
	if err := os.WriteFile(filepath.Join(dir, "fig.png"), figA, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "t"), []byte("do-not-upload"), 0o644); err != nil {
		t.Fatal(err)
	}

	docPath := setupHTMLDoc(t, dir, strings.Join([]string{
		"---",
		"title: HTML Demo",
		"format: html",
		"---",
		"",
		"Math $a*b*c$ and $x_{[1]}(t)$ and price \\$100 and ~~old~~.",
		"",
		`<figure><img src="fig.png" width="80%"><figcaption>Plot</figcaption></figure>`,
		"",
	}, "\n"))

	api := newWriteFakeAPI()
	api.info = map[string]any{"elabftw_version": "6.0.9"}
	cfg := testConfig()

	var out, errBuf bytes.Buffer
	res, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{WorkDir: dir}, &out, &errBuf)
	if err != nil {
		t.Fatalf("first HTML push failed: %v (stderr=%q)", err, errBuf.String())
	}
	if !strings.Contains(res, "pushed") || !strings.Contains(out.String(), "body updated (html)") {
		t.Fatalf("unexpected push output: res=%q out=%q", res, out.String())
	}
	if !strings.Contains(errBuf.String(), "published in html format (one-way)") {
		t.Errorf("expected one-time html publish note on stderr, got %q", errBuf.String())
	}
	if strings.Contains(errBuf.String(), "strips raw HTML") {
		t.Errorf("WarnRawHTMLOnV6 must be suppressed in html format, got %q", errBuf.String())
	}
	for _, name := range api.uploaded {
		if name == "t" {
			t.Fatalf("file 't' inside $x_{[1]}(t)$ must not be uploaded")
		}
	}
	if len(api.patches) != 1 {
		t.Fatalf("expected 1 PATCH, got %d", len(api.patches))
	}
	if api.patches[0]["content_type"] != 1 {
		t.Fatalf("PATCH content_type = %#v, want 1", api.patches[0]["content_type"])
	}
	sentHTML, _ := api.patches[0]["body"].(string)
	for _, want := range []string{"$a*b*c$", `\$100`, "<s>old</s>", `width="80%"`, "app/download.php?"} {
		if !strings.Contains(sentHTML, want) {
			t.Errorf("rendered HTML missing %q:\n%s", want, sentHTML)
		}
	}
	if strings.Contains(sentHTML, "<del>") {
		t.Errorf("rendered HTML must not contain <del>:\n%s", sentHTML)
	}

	saved, err := state.Load("https://e.example", "experiments", "42")
	if err != nil || !sync.IsRegisteredHTML(saved) {
		t.Fatalf("expected registered HTML state (content_type=1), got %#v (err=%v)", saved, err)
	}

	// Second push with no changes must skip PATCH (unchanged) and NOT repeat first-publish note.
	out.Reset()
	errBuf.Reset()
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{WorkDir: dir}, &out, &errBuf); err != nil {
		t.Fatalf("unchanged HTML push failed: %v", err)
	}
	if len(api.patches) != 1 {
		t.Fatalf("expected unchanged push to skip PATCH, got %d patches", len(api.patches))
	}
	if strings.Contains(errBuf.String(), "published in html format (one-way)") {
		t.Errorf("first-publish note should only print once, got %q", errBuf.String())
	}

	// Pre-register a second upload with the same basename fig.png and hash(figB),
	// then replace local fig.png with figB: push MUST NOT treat this as unchanged.
	sumB := sha256.Sum256(figB)
	api.uploads["experiments/42"] = append(api.uploads["experiments/42"], map[string]any{
		"id":             json.Number("200"),
		"real_name":      "fig.png",
		"long_name":      "cd/200-fig.png",
		"storage":        1,
		"hash_algorithm": "sha256",
		"hash":           hex.EncodeToString(sumB[:]),
	})
	if err := os.WriteFile(filepath.Join(dir, "fig.png"), figB, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{WorkDir: dir}, &out, &errBuf); err != nil {
		t.Fatalf("push after switching to second existing upload failed: %v", err)
	}
	if len(api.patches) != 2 {
		t.Fatalf("expected PATCH when selected attachment URL changed from upload A to B, got %d patches", len(api.patches))
	}
	if !strings.Contains(api.patches[1]["body"].(string), "200-fig.png") {
		t.Errorf("expected second PATCH to reference 200-fig.png, got %q", api.patches[1]["body"])
	}
}

func TestHTMLWebEditConflictDeletionLockAndConfirmRemote(t *testing.T) {
	// Web UI edits and external deletions to "" on registered HTML must block both
	// normal push and --force without creating Markdown sidecars, show canonical HTML
	// diff + short sha256, and succeed only with matching --confirm-remote (and never when locked).
	setIsolatedHome(t)
	dir := t.TempDir()
	docPath := setupHTMLDoc(t, dir, "---\ntitle: T\nformat: html\n---\n\n<img src=\"https://e.example/x\" width=\"80%\">\n")
	api := newWriteFakeAPI()
	cfg := testConfig()

	var out, errBuf bytes.Buffer
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{WorkDir: dir}, &out, &errBuf); err != nil {
		t.Fatalf("initial push: %v", err)
	}

	// Simulate Web UI edit changing width to 50%.
	webBody := `<p><img src="https://e.example/x" width="50%"></p>`
	api.entities["experiments/42"]["body"] = webBody

	out.Reset()
	errBuf.Reset()
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{Force: true, WorkDir: dir}, &out, &errBuf); err == nil {
		t.Fatal("expected --force to be refused on Web UI HTML edit")
	}
	if !strings.Contains(errBuf.String(), `width="50%"`) || !strings.Contains(errBuf.String(), sync.ShortHash(sync.BodySHA256(webBody))) {
		t.Errorf("expected canonical diff + short sha256 on stderr, got %q", errBuf.String())
	}
	if _, err := os.Stat(sync.SidecarPath(docPath)); err == nil {
		t.Fatal("HTML conflict must never create .remote.md sidecar")
	}

	// Too-short or wrong confirm hash is rejected.
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{ConfirmRemote: "deadbeef", WorkDir: dir}, &out, &errBuf); err == nil {
		t.Fatal("expected <12-char --confirm-remote to be rejected")
	}
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{ConfirmRemote: "000000000000", WorkDir: dir}, &out, &errBuf); err == nil {
		t.Fatal("expected mismatched --confirm-remote to be rejected")
	}

	// Locked entity must refuse even with exact --confirm-remote and --force.
	api.entities["experiments/42"]["locked"] = 1
	goodHash := sync.ShortHash(sync.BodySHA256(webBody))
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{Force: true, ConfirmRemote: goodHash, WorkDir: dir}, &out, &errBuf); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("expected locked refusal, got %v", err)
	}
	api.entities["experiments/42"]["locked"] = 0

	// Exact --confirm-remote succeeds after unlock and reports body updated.
	out.Reset()
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{ConfirmRemote: goodHash, WorkDir: dir}, &out, &errBuf); err != nil {
		t.Fatalf("confirmed push failed: %v", err)
	}
	if !strings.Contains(out.String(), "body updated (html)") {
		t.Errorf("confirmed push overwriting Web UI edit should report body updated, got %q", out.String())
	}

	// External deletion to empty string "" is a deletion conflict, not fresh creation.
	api.entities["experiments/42"]["body"] = ""
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{Force: true, WorkDir: dir}, &out, &errBuf); err == nil {
		t.Fatal("expected externally emptied registered HTML to refuse --force")
	}
	emptyHash := sync.ShortHash(sync.BodySHA256(""))
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{ConfirmRemote: emptyHash, WorkDir: dir}, &out, &errBuf); err != nil {
		t.Fatalf("confirmed push after external deletion failed: %v", err)
	}
}

func TestHTMLPendingPublishLostResponseRecovery(t *testing.T) {
	// If the first HTML publish PATCH reaches the server but its response is lost,
	// pending_publish prevents permanent lockout while requiring same-user + matching
	// format + exact --confirm-remote, and blocking pull/merge until resolved.
	setIsolatedHome(t)
	dir := t.TempDir()
	docPath := setupHTMLDoc(t, dir, "---\ntitle: Lost\nformat: html\n---\n\nHello HTML\n")
	api := newWriteFakeAPI()
	api.failVerifyOnce = true
	cfg := testConfig()

	var out, errBuf bytes.Buffer
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{WorkDir: dir}, &out, &errBuf); err == nil {
		t.Fatal("expected first push to fail on simulated lost response")
	}
	saved, err := state.Load("https://e.example", "experiments", "42")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sync.PendingPublish(saved); !ok {
		t.Fatalf("expected pending_publish in state after lost HTML response, got %#v", saved)
	}
	if _, err := sync.Pull(context.Background(), docPath, api, cfg, "", &out, &errBuf); err == nil {
		t.Fatal("expected pull to refuse while pending_publish is unresolved")
	}
	if err := sync.Merge(context.Background(), docPath, cfg, "", false, &out); err == nil || !strings.Contains(err.Error(), "html format") {
		t.Fatalf("expected merge to refuse while pending_publish is unresolved, got %v", err)
	}

	remoteBody := api.entities["experiments/42"]["body"].(string)
	remoteHash := sync.ShortHash(sync.BodySHA256(remoteBody))

	// Blind --force is refused.
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{Force: true, WorkDir: dir}, &out, &errBuf); err == nil {
		t.Fatal("expected --force to be refused on unresolved pending_publish")
	}

	// Switching format to markdown with --confirm-remote must NOT recover (bound to TargetCT=1).
	mdContent := strings.Replace(mustRead(t, docPath), "format: html", "format: markdown", 1)
	if err := os.WriteFile(docPath, []byte(mdContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{ConfirmRemote: remoteHash, WorkDir: dir}, &out, &errBuf); err == nil {
		t.Fatal("expected mismatched format: markdown to fail pending_publish recovery")
	}

	// Restoring format: html and passing exact --confirm-remote recovers cleanly.
	htmlContent := strings.Replace(mustRead(t, docPath), "format: markdown", "format: html", 1)
	if err := os.WriteFile(docPath, []byte(htmlContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{ConfirmRemote: remoteHash, WorkDir: dir}, &out, &errBuf); err != nil {
		t.Fatalf("pending_publish recovery failed: %v", err)
	}
	savedAfter, _ := state.Load("https://e.example", "experiments", "42")
	if _, ok := sync.PendingPublish(savedAfter); ok || !sync.IsRegisteredHTML(savedAfter) {
		t.Fatalf("expected pending_publish cleared and HTML registered, got %#v", savedAfter)
	}
}

func TestHTMLPrePatchReGetBlocksConcurrentChangeAndGuardsHold(t *testing.T) {
	// Tests pre-PATCH re-GET catching concurrent body edit / mode switch with identical
	// body bytes / lock, omitted-format refusal on registered HTML, external 1->2 switch
	// guard on pull/push, missing PATCH body rejection, and HTML backtick remote-linked protection.
	setIsolatedHome(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secret.csv"), []byte("topsecret"), 0o644); err != nil {
		t.Fatal(err)
	}
	docPath := setupHTMLDoc(t, dir, "---\ntitle: Guards\nformat: html\n---\n\nInitial\n")
	api := newWriteFakeAPI()
	cfg := testConfig()
	var out, errBuf bytes.Buffer
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{WorkDir: dir}, &out, &errBuf); err != nil {
		t.Fatalf("initial push: %v", err)
	}

	// 1. Pre-PATCH re-GET catches concurrent content_type switch with identical body bytes.
	cur := copyEntityMap(api.entities["experiments/42"])
	switched := copyEntityMap(cur)
	switched["content_type"] = 2
	api.getQueue = []map[string]any{cur, switched}
	updated := strings.Replace(mustRead(t, docPath), "Initial", "Updated", 1)
	if err := os.WriteFile(docPath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	patchesBefore := len(api.patches)
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{WorkDir: dir}, &out, &errBuf); err == nil || !strings.Contains(err.Error(), "remote mode changed") {
		t.Fatalf("expected pre-PATCH re-GET mode-change refusal, got %v", err)
	}
	if len(api.patches) != patchesBefore {
		t.Fatal("PATCH must not run after pre-PATCH re-GET detects mode change")
	}

	// 2. External 1->2 mode switch blocks pull (never writes HTML into local Markdown).
	api.entities["experiments/42"]["content_type"] = 2
	if _, err := sync.Pull(context.Background(), docPath, api, cfg, "", &out, &errBuf); err == nil || !strings.Contains(err.Error(), "markdown mode") {
		t.Fatalf("expected external 1->2 switch to block pull, got %v", err)
	}
	api.entities["experiments/42"]["content_type"] = 1

	// 3. Omitted format: on registered HTML is refused even with --force.
	omitted := strings.Replace(mustRead(t, docPath), "format: html\n", "", 1)
	if err := os.WriteFile(docPath, []byte(omitted), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{Force: true, WorkDir: dir}, &out, &errBuf); err == nil || !strings.Contains(err.Error(), "restore 'format: html'") {
		t.Fatalf("expected omitted format refusal, got %v", err)
	}
	if err := os.WriteFile(docPath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}

	// 4. Remote HTML backtick-wrapped <img src="secret.csv"> blocks uploading local secret.csv.
	btHTML := "<p>`<img src=\"secret.csv\">`</p>"
	api.entities["experiments/42"]["body"] = btHTML
	withSecret := strings.Replace(updated, "Updated", "[s](secret.csv)", 1)
	if err := os.WriteFile(docPath, []byte(withSecret), 0o644); err != nil {
		t.Fatal(err)
	}
	errBuf.Reset()
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{ConfirmRemote: sync.ShortHash(sync.BodySHA256(btHTML)), WorkDir: dir}, &out, &errBuf); err != nil {
		t.Fatalf("confirmed push with backtick HTML secret failed: %v", err)
	}
	for _, name := range api.uploaded {
		if name == "secret.csv" {
			t.Fatal("secret.csv linked from remote HTML inside backticks must not be uploaded")
		}
	}

	// 5. Missing body key in PATCH response aborts before saving state.
	api.patchTransform = func(payload map[string]any) map[string]any {
		return map[string]any{"id": json.Number("42"), "content_type": 1}
	}
	withChange := strings.Replace(withSecret, "[s](secret.csv)", "Body check", 1)
	if err := os.WriteFile(docPath, []byte(withChange), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{WorkDir: dir}, &out, &errBuf); err == nil || !strings.Contains(err.Error(), "valid body field") {
		t.Fatalf("expected missing body field error, got %v", err)
	}
}

func TestHTMLStatusDiffAndPullGuidance(t *testing.T) {
	// Status separates format: html and mode: html (published by elport), prints remote sha256,
	// steers HTML conflicts to diff (not pull); Diff previews canon_html(remote) vs
	// canon_html(render(local)) with UPLOAD_PENDING and prints remote sha256 on stderr.
	setIsolatedHome(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "newplot.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	docPath := setupHTMLDoc(t, dir, "---\ntitle: View\nformat: html\n---\n\nHello **world**\n")
	api := newWriteFakeAPI()
	cfg := testConfig()
	var out, errBuf bytes.Buffer
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{WorkDir: dir}, &out, &errBuf); err != nil {
		t.Fatalf("initial push: %v", err)
	}

	// Simulate Web UI edit + local new image reference.
	webHTML := "<p>Hello <strong>Web</strong></p>\n"
	api.entities["experiments/42"]["body"] = webHTML
	localEdited := strings.Replace(mustRead(t, docPath), "Hello **world**", "Hello **local** ![p](newplot.png)", 1)
	if err := os.WriteFile(docPath, []byte(localEdited), 0o644); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	errBuf.Reset()
	if _, err := sync.Status(context.Background(), docPath, api, cfg, "", &out, &errBuf); err != nil {
		t.Fatalf("Status: %v", err)
	}
	statusOut := out.String()
	for _, want := range []string{"format: html", "mode: html (published by elport)", `remote: changed (use "elport diff")`, "remote sha256:"} {
		if !strings.Contains(statusOut, want) {
			t.Errorf("Status missing %q:\n%s", want, statusOut)
		}
	}

	out.Reset()
	errBuf.Reset()
	if err := sync.Diff(context.Background(), docPath, api, cfg, "", false, &out, &errBuf); err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(out.String(), "UPLOAD_PENDING:newplot.png") || !strings.Contains(out.String(), "-<p>Hello <strong>Web</strong></p>") {
		t.Errorf("unexpected HTML Diff stdout:\n%s", out.String())
	}
	if !strings.Contains(errBuf.String(), "remote sha256: "+sync.ShortHash(sync.BodySHA256(webHTML))) {
		t.Errorf("expected remote sha256 on Diff stderr, got %q", errBuf.String())
	}

	// Pull on registered HTML gives one-way diff/fetch guidance.
	if _, err := sync.Pull(context.Background(), docPath, api, cfg, "", &out, &errBuf); err == nil || !strings.Contains(err.Error(), "elport diff") {
		t.Fatalf("expected pull on registered HTML to point to elport diff, got %v", err)
	}
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestHTMLUnregisteredRefusedEvenWithConfirmRemoteInPhase1(t *testing.T) {
	// Unregistered non-empty HTML entities must remain refused in Phase 1 even with
	// --force or --confirm-remote.
	setIsolatedHome(t)
	dir := t.TempDir()
	docPath := setupHTMLDoc(t, dir, "---\nid: 42\nentity: experiments\nprofile: test\nformat: html\n---\n\nOverwrite\n")
	api := newWriteFakeAPI()
	remoteHTML := "<p>Colleague Web UI note</p>"
	api.entities["experiments/42"] = map[string]any{
		"id":           json.Number("42"),
		"title":        "Colleague",
		"body":         remoteHTML,
		"content_type": 1,
		"userid":       json.Number("99"),
		"team":         json.Number("7"),
	}
	cfg := testConfig()
	var out, errBuf bytes.Buffer
	h := sync.ShortHash(sync.BodySHA256(remoteHTML))
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{Force: true, ConfirmRemote: h, WorkDir: dir}, &out, &errBuf); err == nil {
		t.Fatal("expected unregistered non-empty HTML entity to be refused")
	}
	var htmlErr sync.HtmlEntityError
	_, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{Force: true, WorkDir: dir}, &out, &errBuf)
	if !errors.As(err, &htmlErr) {
		t.Fatalf("expected HtmlEntityError, got %T (%v)", err, err)
	}
}

func TestHTMLNewPushPrePatchReGetSecondLostResponseRemotePathPersistenceAndNoOpDirectURL(t *testing.T) {
	// Regression tests for:
	// - S1: ID-less first HTML publish checks pre-PATCH re-GET and records observed pre_content_type
	// - S2: second lost response during confirmed recovery preserves pending_publish
	// - S3: lost response on registered HTML push removing a remote-linked path persists remote_paths
	// - S7: direct download URL with zero local files loads uploads and skips redundant PATCH
	// - S8: concurrent Push and Pull on the same entity serialize cleanly under -race
	setIsolatedHome(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fig.png"), []byte("fig"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.csv"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	// S1a: ID-less first HTML publish must block when entity is locked during upload.
	docPath := setupHTMLDoc(t, dir, "---\ntitle: Race\nformat: html\n---\n\n![f](fig.png)\n")
	api := newWriteFakeAPI()
	api.onUpload = func(entity string, eid int, _ string) {
		api.entities[entity+"/42"]["locked"] = 1
	}
	cfg := testConfig()
	var out, errBuf bytes.Buffer
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{WorkDir: dir}, &out, &errBuf); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("expected ID-less HTML push to catch lock during upload, got %v", err)
	}
	if len(api.patches) != 0 {
		t.Fatalf("PATCH must not run when entity locked during upload, got %d patches", len(api.patches))
	}
	// S1b: entity created with content_type=2 stayed unchanged while upload aborted;
	// unlocking and retrying without --confirm-remote must succeed cleanly.
	api.onUpload = nil
	api.entities["experiments/42"]["locked"] = 0

	// S2: lose response TWICE (initial + first confirmed recovery) -> pending_publish MUST survive
	// so third attempt with matching --confirm-remote still recovers cleanly.
	api.failVerifyOnce = true
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{WorkDir: dir}, &out, &errBuf); err == nil {
		t.Fatal("expected first lost-response failure")
	}
	h1 := sync.ShortHash(sync.BodySHA256(api.entities["experiments/42"]["body"].(string)))
	api.failVerifyOnce = true
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{ConfirmRemote: h1, WorkDir: dir}, &out, &errBuf); err == nil {
		t.Fatal("expected second lost-response failure")
	}
	savedMid, _ := state.Load("https://e.example", "experiments", "42")
	if _, ok := sync.PendingPublish(savedMid); !ok {
		t.Fatalf("pending_publish must survive second lost response, got %#v", savedMid)
	}
	h2 := sync.ShortHash(sync.BodySHA256(api.entities["experiments/42"]["body"].(string)))
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{ConfirmRemote: h2, WorkDir: dir}, &out, &errBuf); err != nil {
		t.Fatalf("third confirmed recovery failed: %v", err)
	}

	// S3: registered HTML push removing bt_html_secret loses PATCH response ->
	// remote_paths MUST already be persisted so next push referencing secret.csv still blocks upload.
	btHTML := "<p>`<img src=\"secret.csv\">`</p>"
	api.entities["experiments/42"]["body"] = btHTML
	api.failVerifyOnce = true
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{ConfirmRemote: sync.ShortHash(sync.BodySHA256(btHTML)), WorkDir: dir}, &out, &errBuf); err == nil {
		t.Fatal("expected lost response on registered HTML push")
	}
	withSecret := strings.Replace(mustRead(t, docPath), "![f](fig.png)", "[s](secret.csv)", 1)
	if err := os.WriteFile(docPath, []byte(withSecret), 0o644); err != nil {
		t.Fatal(err)
	}
	h3 := sync.ShortHash(sync.BodySHA256(api.entities["experiments/42"]["body"].(string)))
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{ConfirmRemote: h3, WorkDir: dir}, &out, &errBuf); err != nil {
		t.Fatalf("confirmed retry after S3 lost response failed: %v", err)
	}
	for _, name := range api.uploaded {
		if name == "secret.csv" {
			t.Fatal("secret.csv removed during lost-response HTML push must remain excluded from upload")
		}
	}

	// S7: direct download URL in body (zero local files) must load uploads and skip redundant PATCH.
	dlURL := "https://e.example/app/download.php?f=ab%2F100-fig.png&name=fig.png&storage=1"
	api.uploads["experiments/42"] = []map[string]any{{
		"id":             json.Number("100"),
		"real_name":      "fig.png",
		"long_name":      "ab/100-fig.png",
		"storage":        1,
		"hash_algorithm": "sha256",
		"hash":           strings.Repeat("a", 64),
	}}
	directDoc := strings.Replace(mustRead(t, docPath), "[s](secret.csv)", "![f]("+dlURL+")", 1)
	if err := os.WriteFile(docPath, []byte(directDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{WorkDir: dir}, &out, &errBuf); err != nil {
		t.Fatalf("push with direct URL failed: %v", err)
	}
	patchesBeforeNoOp := len(api.patches)
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{WorkDir: dir}, &out, &errBuf); err != nil {
		t.Fatalf("unchanged push with direct URL failed: %v", err)
	}
	if len(api.patches) != patchesBeforeNoOp {
		t.Fatalf("expected unchanged push with direct download URL to skip PATCH, got %d new patch(es)", len(api.patches)-patchesBeforeNoOp)
	}

	// S8: deterministic creation-path concurrency test — an ID-less first HTML Push
	// locks the new entity before exposing the ID on disk, so a concurrent Pull
	// started as soon as the ID is written deterministically blocks on the entity
	// lock and cannot clobber the registered HTML state.
	concDir := t.TempDir()
	concDoc := setupHTMLDoc(t, concDir, "---\ntitle: Conc\nformat: html\n---\n\nConcurrent HTML\n")
	concAPI := newWriteFakeAPI()
	concAPI.nextID = 88
	pushEntered := make(chan struct{})
	releasePush := make(chan struct{})
	firstGet := true
	concAPI.onGet = func(entity string, eid int) {
		if eid == 88 && firstGet {
			firstGet = false
			close(pushEntered)
			<-releasePush
		}
	}
	contended := make(chan struct{})
	var contendOnce gosync.Once
	restoreContend := state.OnLockContendForTest(func() {
		contendOnce.Do(func() { close(contended) })
	})
	defer restoreContend()
	pushDone := make(chan error, 1)
	go func() {
		var po, pe bytes.Buffer
		_, perr := sync.Push(context.Background(), concDoc, concAPI, cfg, sync.PushOptions{WorkDir: concDir}, &po, &pe)
		pushDone <- perr
	}()
	select {
	case <-pushEntered:
	case err := <-pushDone:
		t.Fatalf("concurrent Push exited before GET: %v", err)
	}
	pullDone := make(chan error, 1)
	go func() {
		var lo, le bytes.Buffer
		_, lerr := sync.Pull(context.Background(), concDoc, concAPI, cfg, "", &lo, &le)
		pullDone <- lerr
	}()
	select {
	case <-contended:
	case err := <-pullDone:
		close(releasePush)
		<-pushDone
		t.Fatalf("Pull must block behind ID-less Push lock, finished early with %v", err)
	}
	close(releasePush)
	if err := <-pushDone; err != nil {
		t.Fatalf("concurrent Push failed: %v", err)
	}
	if err := <-pullDone; err == nil {
		t.Fatal("Pull after registered HTML Push should see HTML state and refuse")
	}
	concSaved, err := state.Load("https://e.example", "experiments", "88")
	if err != nil || !sync.IsRegisteredHTML(concSaved) {
		t.Fatalf("registered HTML state must survive concurrent Pull, got %#v (err=%v)", concSaved, err)
	}

	// S11 & S12: first HTML push blocked at pre-PATCH re-GET because Web UI filled
	// the empty entity during upload must NOT leave a pending_publish marker, must
	// not suggest --confirm-remote when the entity started in HTML mode and only its
	// body changed, and a later confirmed retry must still refuse unregistered HTML.
	s11Dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(s11Dir, "fig.png"), []byte("fig"), 0o644); err != nil {
		t.Fatal(err)
	}
	s11Doc := setupHTMLDoc(t, s11Dir, "---\nid: 99\nentity: experiments\nprofile: test\nformat: html\n---\n\n![f](fig.png)\n")
	s11API := newWriteFakeAPI()
	s11API.entities["experiments/99"] = map[string]any{
		"id":           json.Number("99"),
		"title":        "S11",
		"body":         "",
		"content_type": 1,
		"userid":       json.Number("1"),
		"team":         json.Number("7"),
	}
	webFilled := "<p>Colleague filled this in Web UI</p>"
	s11API.onUpload = func(entity string, eid int, _ string) {
		s11API.entities[entity+"/99"]["body"] = webFilled
	}
	var s11Out, s11Err bytes.Buffer
	var s12HtmlErr sync.HtmlEntityError
	_, firstErr := sync.Push(context.Background(), s11Doc, s11API, cfg, sync.PushOptions{WorkDir: s11Dir}, &s11Out, &s11Err)
	if firstErr == nil {
		t.Fatal("expected S11/S12 first push to abort before PATCH")
	}
	if strings.Contains(firstErr.Error(), "--confirm-remote") {
		t.Fatalf("S12 abort on unregistered HTML must not suggest --confirm-remote, got: %v", firstErr)
	}
	if !errors.As(firstErr, &s12HtmlErr) {
		t.Fatalf("expected S12 abort error to wrap HtmlEntityError, got: %v", firstErr)
	}
	if len(s11API.patches) != 0 {
		t.Fatalf("expected 0 PATCHes on S11/S12 abort, got %d", len(s11API.patches))
	}
	s11Saved, _ := state.Load("https://e.example", "experiments", "99")
	if _, hasPP := sync.PendingPublish(s11Saved); hasPP {
		t.Fatalf("S11 abort before PATCH must not leave pending_publish in state: %#v", s11Saved)
	}
	s11API.onUpload = nil
	s11Hash := sync.ShortHash(sync.BodySHA256(webFilled))
	var s11HtmlErr sync.HtmlEntityError
	if _, err := sync.Push(context.Background(), s11Doc, s11API, cfg, sync.PushOptions{ConfirmRemote: s11Hash, WorkDir: s11Dir}, &s11Out, &s11Err); !errors.As(err, &s11HtmlErr) {
		t.Fatalf("expected S11 confirmed retry to refuse unregistered Web UI HTML (HtmlEntityError), got %v", err)
	}

	// Also verify the ID-less S11 path (POST creates Markdown entity, Web UI edits to HTML during upload).
	s11bDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(s11bDir, "fig.png"), []byte("fig"), 0o644); err != nil {
		t.Fatal(err)
	}
	s11bDoc := setupHTMLDoc(t, s11bDir, "---\ntitle: S11b\nformat: html\n---\n\n![f](fig.png)\n")
	s11bAPI := newWriteFakeAPI()
	s11bAPI.nextID = 100
	s11bAPI.onUpload = func(entity string, eid int, _ string) {
		s11bAPI.entities[entity+"/100"]["content_type"] = 1
		s11bAPI.entities[entity+"/100"]["body"] = webFilled
	}
	if _, err := sync.Push(context.Background(), s11bDoc, s11bAPI, cfg, sync.PushOptions{WorkDir: s11bDir}, &s11Out, &s11Err); err == nil {
		t.Fatal("expected ID-less S11 first push to abort before PATCH")
	}
	if len(s11bAPI.patches) != 0 {
		t.Fatalf("expected 0 PATCHes on ID-less S11 abort, got %d", len(s11bAPI.patches))
	}
	s11bSaved, _ := state.Load("https://e.example", "experiments", "100")
	if _, hasPP := sync.PendingPublish(s11bSaved); hasPP {
		t.Fatalf("ID-less S11 abort before PATCH must not leave pending_publish in state: %#v", s11bSaved)
	}
	s11bAPI.onUpload = nil
	if _, err := sync.Push(context.Background(), s11bDoc, s11bAPI, cfg, sync.PushOptions{ConfirmRemote: s11Hash, WorkDir: s11bDir}, &s11Out, &s11Err); !errors.As(err, &s11HtmlErr) {
		t.Fatalf("expected ID-less S11 confirmed retry to refuse unregistered HTML (HtmlEntityError), got %v", err)
	}
}
