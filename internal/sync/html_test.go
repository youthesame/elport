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

	setIsolatedHome(t)
	dir := t.TempDir()
	docPath := setupHTMLDoc(t, dir, "---\ntitle: T\nformat: html\n---\n\n<img src=\"https://e.example/x\" width=\"80%\">\n")
	api := newWriteFakeAPI()
	cfg := testConfig()

	var out, errBuf bytes.Buffer
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{WorkDir: dir}, &out, &errBuf); err != nil {
		t.Fatalf("initial push: %v", err)
	}

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

	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{ConfirmRemote: "deadbeef", WorkDir: dir}, &out, &errBuf); err == nil {
		t.Fatal("expected <12-char --confirm-remote to be rejected")
	}
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{ConfirmRemote: "000000000000", WorkDir: dir}, &out, &errBuf); err == nil {
		t.Fatal("expected mismatched --confirm-remote to be rejected")
	}

	api.entities["experiments/42"]["locked"] = 1
	goodHash := sync.ShortHash(sync.BodySHA256(webBody))
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{Force: true, ConfirmRemote: goodHash, WorkDir: dir}, &out, &errBuf); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("expected locked refusal, got %v", err)
	}
	api.entities["experiments/42"]["locked"] = 0

	out.Reset()
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{ConfirmRemote: goodHash, WorkDir: dir}, &out, &errBuf); err != nil {
		t.Fatalf("confirmed push failed: %v", err)
	}
	if !strings.Contains(out.String(), "body updated (html)") {
		t.Errorf("confirmed push overwriting Web UI edit should report body updated, got %q", out.String())
	}

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

	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{Force: true, WorkDir: dir}, &out, &errBuf); err == nil {
		t.Fatal("expected --force to be refused on unresolved pending_publish")
	}

	mdContent := strings.Replace(mustRead(t, docPath), "format: html", "format: markdown", 1)
	if err := os.WriteFile(docPath, []byte(mdContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{ConfirmRemote: remoteHash, WorkDir: dir}, &out, &errBuf); err == nil {
		t.Fatal("expected mismatched format: markdown to fail pending_publish recovery")
	}

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

	api.entities["experiments/42"]["content_type"] = 2
	if _, err := sync.Pull(context.Background(), docPath, api, cfg, "", &out, &errBuf); err == nil || !strings.Contains(err.Error(), "markdown mode") {
		t.Fatalf("expected external 1->2 switch to block pull, got %v", err)
	}
	api.entities["experiments/42"]["content_type"] = 1

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

	out.Reset()
	errBuf.Reset()
	if _, err := sync.Pull(context.Background(), docPath, api, cfg, "", &out, &errBuf); err != nil {
		t.Fatalf("expected metadata-only pull on registered HTML to succeed, got %v", err)
	}
	if !strings.Contains(out.String(), "html; body untouched") || !strings.Contains(errBuf.String(), "elport diff") {
		t.Fatalf("unexpected metadata-only pull output: out=%q err=%q", out.String(), errBuf.String())
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

	setIsolatedHome(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fig.png"), []byte("fig"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.csv"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

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

	api.onUpload = nil
	api.entities["experiments/42"]["locked"] = 0

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
	if err := <-pullDone; err != nil {
		t.Fatalf("Pull after registered HTML Push should run metadata-only pull cleanly, got %v", err)
	}
	concSaved, err := state.Load("https://e.example", "experiments", "88")
	if err != nil || !sync.IsRegisteredHTML(concSaved) {
		t.Fatalf("registered HTML state must survive concurrent Pull, got %#v (err=%v)", concSaved, err)
	}

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
	if _, err := sync.Push(context.Background(), s11Doc, s11API, cfg, sync.PushOptions{ConfirmRemote: s11Hash, WorkDir: s11Dir}, &s11Out, &s11Err); err != nil {
		t.Fatalf("expected owner retry with exact --confirm-remote to adopt unregistered HTML, got %v", err)
	}

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
		s11bAPI.entities[entity+"/100"]["userid"] = json.Number("99")
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
		t.Fatalf("expected ID-less S11 non-owner confirmed retry to refuse unregistered HTML (HtmlEntityError), got %v", err)
	}
}

func TestHTMLPhase2MigrateFromMarkdownAndLegacyBase(t *testing.T) {

	setIsolatedHome(t)
	dir := t.TempDir()
	docPath := setupHTMLDoc(t, dir, "---\ntitle: Migrate\n---\n\nHello **world**\n")
	api := newWriteFakeAPI()
	cfg := testConfig()
	var out, errBuf bytes.Buffer

	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{WorkDir: dir}, &out, &errBuf); err != nil {
		t.Fatalf("initial md push: %v", err)
	}

	saved, err := state.Load("https://e.example", "experiments", "42")
	if err != nil {
		t.Fatal(err)
	}
	delete(saved, "content_type")
	if err := state.Save("https://e.example", "experiments", "42", saved); err != nil {
		t.Fatal(err)
	}

	withHTML := strings.Replace(mustRead(t, docPath), "title: Migrate\n", "title: Migrate\nformat: html\n", 1)
	if err := os.WriteFile(docPath, []byte(withHTML), 0o644); err != nil {
		t.Fatal(err)
	}

	origRemote := api.entities["experiments/42"]["body"].(string)
	api.entities["experiments/42"]["body"] = "Web edited Markdown\n"
	hWeb := sync.ShortHash(sync.BodySHA256("Web edited Markdown\n"))
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{Force: true, ConfirmRemote: hWeb, WorkDir: dir}, &out, &errBuf); err == nil || !strings.Contains(err.Error(), "remote changed") {
		t.Fatalf("expected conflict refusing 2->1 migration on diverged remote Markdown, got %v", err)
	}
	if _, err := os.Stat(sync.SidecarPath(docPath)); err == nil {
		t.Fatal("2->1 migration conflict must not create .remote.md sidecar")
	}

	api.entities["experiments/42"]["body"] = origRemote
	out.Reset()
	errBuf.Reset()
	if err := sync.Diff(context.Background(), docPath, api, cfg, "", false, &out, &errBuf); err != nil {
		t.Fatalf("Diff before 2->1 migration: %v", err)
	}
	if !strings.Contains(out.String(), "-Hello **world**") || !strings.Contains(out.String(), "+<p>Hello <strong>world</strong></p>") {
		t.Errorf("unexpected 2->1 migration diff stdout:\n%s", out.String())
	}
	if !strings.Contains(errBuf.String(), "remote sha256: "+sync.ShortHash(sync.BodySHA256(origRemote))) {
		t.Errorf("missing remote sha256 on 2->1 diff stderr: %q", errBuf.String())
	}

	api.failVerifyOnce = true
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{WorkDir: dir}, &out, &errBuf); err == nil {
		t.Fatal("expected lost-response error on 2->1 migration")
	}
	savedLost, _ := state.Load("https://e.example", "experiments", "42")
	pp, ok := sync.PendingPublish(savedLost)
	if !ok || pp.TargetCT != 1 || pp.PreCT != 2 {
		t.Fatalf("expected pending_publish target=1 pre=2, got %+v (ok=%v)", pp, ok)
	}
	if savedLost["remote_base"] != origRemote {
		t.Fatalf("expected pre-PATCH Markdown base preserved, got %#v", savedLost["remote_base"])
	}
	migratedRemote := api.entities["experiments/42"]["body"].(string)
	hMig := sync.ShortHash(sync.BodySHA256(migratedRemote))

	out.Reset()
	errBuf.Reset()
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{ConfirmRemote: hMig, WorkDir: dir}, &out, &errBuf); err != nil {
		t.Fatalf("2->1 lost-response recovery failed: %v", err)
	}
	if !strings.Contains(out.String(), "body updated (html)") || !strings.Contains(errBuf.String(), "published in html format (one-way)") {
		t.Errorf("unexpected 2->1 output: out=%q err=%q", out.String(), errBuf.String())
	}
	savedDone, _ := state.Load("https://e.example", "experiments", "42")
	if !sync.IsRegisteredHTML(savedDone) {
		t.Fatalf("expected registered HTML after 2->1 migration, got %#v", savedDone)
	}

	dir2 := t.TempDir()
	doc2 := setupHTMLDoc(t, dir2, "---\nid: 55\nentity: experiments\nprofile: test\nformat: html\n---\n\nNo base\n")
	api.entities["experiments/55"] = map[string]any{"id": json.Number("55"), "body": "Existing MD\n", "content_type": 2, "userid": json.Number("1"), "team": json.Number("7")}
	if _, err := sync.Push(context.Background(), doc2, api, cfg, sync.PushOptions{Force: true, WorkDir: dir2}, &out, &errBuf); err == nil || !strings.Contains(err.Error(), "base unavailable") {
		t.Fatalf("expected base unavailable refusal for 2->1 without base, got %v", err)
	}
}

func TestHTMLPhase2RevertToMarkdownAndPendingRecovery(t *testing.T) {

	setIsolatedHome(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secret.csv"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	docPath := setupHTMLDoc(t, dir, "---\ntitle: Revert\nformat: html\n---\n\nHello **local**\n")
	api := newWriteFakeAPI()
	cfg := testConfig()
	var out, errBuf bytes.Buffer

	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{WorkDir: dir}, &out, &errBuf); err != nil {
		t.Fatalf("initial html push: %v", err)
	}

	emptyDoc := strings.Replace(mustRead(t, docPath), "Hello **local**\n", "", 1)
	if err := os.WriteFile(docPath, []byte(emptyDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{Force: true, WorkDir: dir}, &out, &errBuf); err != nil {
		t.Fatalf("push empty html body with --force: %v", err)
	}
	omittedEmpty := strings.Replace(mustRead(t, docPath), "format: html\n", "", 1)
	if err := os.WriteFile(docPath, []byte(omittedEmpty), 0o644); err != nil {
		t.Fatal(err)
	}
	var htmlErr sync.HtmlEntityError
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{Force: true, ConfirmRemote: sync.ShortHash(sync.BodySHA256("")), WorkDir: dir}, &out, &errBuf); !errors.As(err, &htmlErr) {
		t.Fatalf("expected HtmlEntityError for omitted format on empty registered HTML, got %v", err)
	}

	btHTML := "<p>`<img src=\"secret.csv\">`</p>"
	api.entities["experiments/42"]["body"] = btHTML
	mdDoc := strings.Replace(mustRead(t, docPath), "title: Revert\n", "title: Revert\nformat: markdown\n", 1) + "Hello **local**\n"
	if err := os.WriteFile(docPath, []byte(mdDoc), 0o644); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	errBuf.Reset()
	if err := sync.Diff(context.Background(), docPath, api, cfg, "", false, &out, &errBuf); err != nil {
		t.Fatalf("Diff on 1->2 revert: %v", err)
	}
	if !strings.Contains(out.String(), "+Hello **local**") || strings.Contains(out.String(), "<strong>local</strong>") {
		t.Errorf("1->2 Diff must show outgoing Markdown, got:\n%s", out.String())
	}
	if !strings.Contains(errBuf.String(), "remote sha256: "+sync.ShortHash(sync.BodySHA256(btHTML))) {
		t.Errorf("1->2 Diff missing remote sha256: %q", errBuf.String())
	}

	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{Force: true, WorkDir: dir}, &out, &errBuf); err == nil {
		t.Fatal("expected --force refusal on diverged 1->2 revert")
	}

	api.failVerifyOnce = true
	hBT := sync.ShortHash(sync.BodySHA256(btHTML))
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{ConfirmRemote: hBT, WorkDir: dir}, &out, &errBuf); err == nil {
		t.Fatal("expected simulated lost response on 1->2 revert")
	}
	savedLost, _ := state.Load("https://e.example", "experiments", "42")
	pp, ok := sync.PendingPublish(savedLost)
	if !ok || pp.TargetCT != 2 || pp.PreCT != 1 {
		t.Fatalf("expected pending_publish target=2 pre=1, got %+v (ok=%v)", pp, ok)
	}

	curRemote := api.entities["experiments/42"]["body"].(string)
	hCur := sync.ShortHash(sync.BodySHA256(curRemote))
	htmlAgain := strings.Replace(mustRead(t, docPath), "format: markdown", "format: html", 1)
	if err := os.WriteFile(docPath, []byte(htmlAgain), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{ConfirmRemote: hCur, WorkDir: dir}, &out, &errBuf); err == nil {
		t.Fatal("expected format: html to be rejected for target=2 pending_publish")
	}

	withSecret := strings.Replace(mdDoc, "Hello **local**", "[s](secret.csv)", 1)
	if err := os.WriteFile(docPath, []byte(withSecret), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errBuf.Reset()
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{ConfirmRemote: hCur, WorkDir: dir}, &out, &errBuf); err != nil {
		t.Fatalf("1->2 recovery failed: %v", err)
	}
	if !strings.Contains(out.String(), "body updated (markdown)") || !strings.Contains(out.String(), "confirmed: https://e.example experiments/42") {
		t.Errorf("unexpected 1->2 recovery stdout: %q", out.String())
	}
	for _, name := range api.uploaded {
		if name == "secret.csv" {
			t.Fatal("secret.csv from pre-PATCH HTML remote must remain excluded after 1->2 revert recovery")
		}
	}
	savedMD, _ := state.Load("https://e.example", "experiments", "42")
	if sync.SavedContentType(savedMD) != 2 {
		t.Fatalf("expected content_type=2 after 1->2 revert, got %#v", savedMD)
	}

	api.entities["experiments/42"]["body"] = "Pulled after revert\n"
	if _, err := sync.Pull(context.Background(), docPath, api, cfg, "", &out, &errBuf); err != nil {
		t.Fatalf("pull after 1->2 revert failed: %v", err)
	}
	if !strings.Contains(mustRead(t, docPath), "Pulled after revert") {
		t.Fatalf("expected pulled Markdown body after revert, got %q", mustRead(t, docPath))
	}

	docPath2 := setupHTMLDoc(t, t.TempDir(), "---\ntitle: Ext\nformat: html\n---\n\nBody\n")
	api2 := newWriteFakeAPI()
	if _, err := sync.Push(context.Background(), docPath2, api2, cfg, sync.PushOptions{WorkDir: filepath.Dir(docPath2)}, &out, &errBuf); err != nil {
		t.Fatal(err)
	}
	api2.entities["experiments/42"]["content_type"] = 2
	mdExt := strings.Replace(mustRead(t, docPath2), "format: html", "format: markdown", 1)
	if err := os.WriteFile(docPath2, []byte(mdExt), 0o644); err != nil {
		t.Fatal(err)
	}
	extBody := api2.entities["experiments/42"]["body"].(string)
	api2.failPatchOnce = true
	if _, err := sync.Push(context.Background(), docPath2, api2, cfg, sync.PushOptions{ConfirmRemote: sync.ShortHash(sync.BodySHA256(extBody)), WorkDir: filepath.Dir(docPath2)}, &out, &errBuf); err == nil {
		t.Fatal("expected simulated patch failure")
	}
	savedExt, _ := state.Load("https://e.example", "experiments", "42")
	if _, hasPP := sync.PendingPublish(savedExt); hasPP {
		t.Fatalf("confirmed MD->MD write after external 1->2 switch must not set pending_publish: %#v", savedExt)
	}
}

func TestHTMLPhase2SecondMachineOwnerAdoptionAndGuards(t *testing.T) {

	setIsolatedHome(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secret.csv"), []byte("topsecret"), 0o644); err != nil {
		t.Fatal(err)
	}
	docPath := setupHTMLDoc(t, dir, "---\nid: 70\nentity: experiments\nprofile: test\nformat: html\n---\n\nSecond machine body\n")
	btHTML := "<p>`<img src=\"secret.csv\">`</p>"
	hBT := sync.ShortHash(sync.BodySHA256(btHTML))

	api := newWriteFakeAPI()
	api.entities["experiments/70"] = map[string]any{
		"id":           json.Number("70"),
		"title":        "Remote HTML",
		"body":         btHTML,
		"content_type": 1,
		"userid":       json.Number("99"),
		"team":         json.Number("7"),
	}
	cfg := testConfig()
	var out, errBuf bytes.Buffer
	var htmlErr sync.HtmlEntityError

	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{Force: true, ConfirmRemote: hBT, WorkDir: dir}, &out, &errBuf); !errors.As(err, &htmlErr) {
		t.Fatalf("expected HtmlEntityError for non-owner adoption, got %v", err)
	}

	for _, badUID := range []any{nil, 0, -1, "", "0", "abc", true} {
		api.entities["experiments/70"]["userid"] = badUID
		api.me["userid"] = badUID
		if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{ConfirmRemote: hBT, WorkDir: dir}, &out, &errBuf); !errors.As(err, &htmlErr) {
			t.Fatalf("expected HtmlEntityError for invalid userid %#v, got %v", badUID, err)
		}
	}
	api.me["userid"] = json.Number("1")
	api.entities["experiments/70"]["userid"] = json.Number("1")

	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{Force: true, WorkDir: dir}, &out, &errBuf); !errors.As(err, &htmlErr) {
		t.Fatalf("expected HtmlEntityError for owner without --confirm-remote, got %v", err)
	}

	preDoc := copyEntityMap(api.entities["experiments/70"])
	regetDoc := copyEntityMap(preDoc)
	regetDoc["userid"] = json.Number("99")
	api.getQueue = []map[string]any{preDoc, regetDoc}
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{ConfirmRemote: hBT, WorkDir: dir}, &out, &errBuf); !errors.As(err, &htmlErr) {
		t.Fatalf("expected HtmlEntityError when ownership changed at pre-PATCH re-GET, got %v", err)
	}
	if len(api.patches) != 0 {
		t.Fatalf("expected 0 PATCHes after ownership re-GET refusal, got %d", len(api.patches))
	}

	api.failVerifyOnce = true
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{ConfirmRemote: hBT, WorkDir: dir}, &out, &errBuf); err == nil {
		t.Fatal("expected lost-response error on adoption PATCH")
	}
	savedAfterLost, _ := state.Load("https://e.example", "experiments", "70")
	if sync.HasBase(savedAfterLost) {
		t.Fatalf("lost adoption must not record a base: %#v", savedAfterLost)
	}
	if _, hasPP := sync.PendingPublish(savedAfterLost); hasPP {
		t.Fatalf("lost adoption must not record pending_publish: %#v", savedAfterLost)
	}
	newBody := api.entities["experiments/70"]["body"].(string)
	hNew := sync.ShortHash(sync.BodySHA256(newBody))

	api.entities["experiments/70"]["userid"] = json.Number("99")
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{ConfirmRemote: hNew, WorkDir: dir}, &out, &errBuf); !errors.As(err, &htmlErr) {
		t.Fatalf("expected non-owner refusal after lost adoption, got %v", err)
	}

	api.entities["experiments/70"]["userid"] = json.Number("1")
	withSecret := strings.Replace(mustRead(t, docPath), "Second machine body", "[s](secret.csv)", 1)
	if err := os.WriteFile(docPath, []byte(withSecret), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errBuf.Reset()
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{ConfirmRemote: hNew, WorkDir: dir}, &out, &errBuf); err != nil {
		t.Fatalf("owner adoption failed: %v", err)
	}
	for _, name := range api.uploaded {
		if name == "secret.csv" {
			t.Fatal("secret.csv from pre-adoption remote HTML must not be uploaded")
		}
	}
	if !strings.Contains(out.String(), "confirmed: https://e.example experiments/70 (html → html)") {
		t.Errorf("adoption output missing instance/entity/mode confirmation: %q", out.String())
	}
	savedAdopted, _ := state.Load("https://e.example", "experiments", "70")
	if !sync.IsRegisteredHTML(savedAdopted) {
		t.Fatalf("expected registered HTML after adoption, got %#v", savedAdopted)
	}
}

func TestHTMLPhase2MetadataOnlyPullPreservesCRLFBodiesAndUnionsTags(t *testing.T) {

	setIsolatedHome(t)
	dir := t.TempDir()
	crlfBody := "Line one\r\nLine two with **bold**\r\n"
	initialDoc := "---\r\nid: 81\r\nentity: experiments\r\nprofile: test\r\nformat: html\r\ntitle: Local Title\r\ncategory: Old Cat\r\ntags:\r\n- local\r\n---\r\n\r\n" + crlfBody
	docPath := setupHTMLDoc(t, dir, initialDoc)
	if err := state.Save("https://e.example", "experiments", "81", map[string]any{
		"remote_base":  "<p>base html</p>",
		"local_base":   "Line one\nLine two with **bold**\n",
		"content_type": 1,
		"meta_base":    map[string]any{"title": "Old Title", "category": "Old Cat"},
		"remote_paths": []string{"secret.csv"},
		"path":         docPath,
	}); err != nil {
		t.Fatal(err)
	}

	api := newWriteFakeAPI()
	remoteHTML := "<p>Web edited HTML</p>"
	api.entities["experiments/81"] = map[string]any{
		"id":             json.Number("81"),
		"body":           remoteHTML,
		"content_type":   1,
		"title":          "Web Title",
		"category_title": "Web Cat",
		"status_title":   "Running",
		"tags":           "web|shared",
		"userid":         json.Number("1"),
		"team":           json.Number("7"),
	}
	api.uploads["experiments/81"] = []map[string]any{
		{"id": json.Number("300"), "real_name": "unused.bin", "long_name": "ab/300-unused.bin", "storage": 1},
	}
	cfg := testConfig()
	var out, errBuf bytes.Buffer
	res, err := sync.Pull(context.Background(), docPath, api, cfg, "", &out, &errBuf)
	if err != nil {
		t.Fatalf("metadata-only pull failed: %v", err)
	}
	if res != "pulled metadata (html)" {
		t.Errorf("pull summary = %q, want %q", res, "pulled metadata (html)")
	}
	for _, c := range api.calls {
		if c == "uploads" {
			t.Fatalf("metadata-only pull must not fetch uploads, calls=%v", api.calls)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "unused.bin")); err == nil {
		t.Fatal("metadata-only pull must not download attachments")
	}
	rawAfter, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(rawAfter), "\n\n"+crlfBody) {
		t.Fatalf("metadata-only pull did not preserve exact CRLF body bytes:\ngot=%q", string(rawAfter))
	}
	docAfter, err := sync.ReadDocument(docPath, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if docAfter.Meta.Values["title"] != "Local Title" {
		t.Errorf("local title edit lost: %#v", docAfter.Meta.Values["title"])
	}
	if docAfter.Meta.Values["category"] != "Web Cat" || docAfter.Meta.Values["status"] != "Running" {
		t.Errorf("remote metadata not pulled: %#v", docAfter.Meta.Values)
	}
	if docAfter.Meta.Values["format"] != "html" {
		t.Errorf("format: html lost: %#v", docAfter.Meta.Values["format"])
	}
	if !strings.Contains(errBuf.String(), "remote HTML body also changed") || !strings.Contains(errBuf.String(), sync.ShortHash(sync.BodySHA256(remoteHTML))) {
		t.Errorf("expected remote body changed note with sha256 on stderr, got %q", errBuf.String())
	}
	savedAfter, _ := state.Load("https://e.example", "experiments", "81")
	if savedAfter["remote_base"] != "<p>base html</p>" || sync.SavedContentType(savedAfter) != 1 {
		t.Fatalf("metadata-only pull must preserve remote_base and content_type: %#v", savedAfter)
	}

	api.entities["experiments/81"]["category_title"] = "Next Cat"
	api.onGet = func(_ string, _ int) {
		_ = os.WriteFile(docPath, []byte(strings.Replace(string(rawAfter), "Line one", "Concurrent edit", 1)), 0o644)
	}
	if _, err := sync.Pull(context.Background(), docPath, api, cfg, "", &out, &errBuf); err == nil || !strings.Contains(err.Error(), "local document changed") {
		t.Fatalf("expected concurrent local edit refusal, got %v", err)
	}
	if !strings.Contains(mustRead(t, docPath), "Concurrent edit") {
		t.Fatal("concurrent local edit was overwritten during metadata-only pull")
	}
}

func TestHTMLStaleConfirmRemoteRejectedBeforeSideEffectsAndPendingStatusSteersToDiff(t *testing.T) {
	setIsolatedHome(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "newfig.png"), []byte("png-data"), 0o644); err != nil {
		t.Fatal(err)
	}
	docPath := setupHTMLDoc(t, dir, "---\ntitle: S1\nformat: html\nread: team\n---\n\nInitial\n")
	api := newWriteFakeAPI()
	cfg := testConfig()
	var out, errBuf bytes.Buffer
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{WorkDir: dir}, &out, &errBuf); err != nil {
		t.Fatalf("initial push: %v", err)
	}
	api.entities["experiments/42"]["canread_base"] = 30
	revertDoc := strings.Replace(mustRead(t, docPath), "format: html", "format: markdown", 1)
	revertDoc = strings.Replace(revertDoc, "read: team", "read: owner", 1)
	revertDoc = strings.Replace(revertDoc, "Initial", "![f](newfig.png)", 1)
	if err := os.WriteFile(docPath, []byte(revertDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	patchesBefore := len(api.patches)
	staleHash := "000000000000"
	out.Reset()
	errBuf.Reset()
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{DryRun: true, ConfirmRemote: staleHash, WorkDir: dir}, &out, &errBuf); err == nil {
		t.Fatal("expected dry-run with stale --confirm-remote on matching-base 1->2 revert to fail")
	}
	if strings.Contains(out.String(), "confirmed:") {
		t.Errorf("dry-run must not report confirmed for stale hash: %q", out.String())
	}
	if _, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{ConfirmRemote: staleHash, WorkDir: dir}, &out, &errBuf); err == nil {
		t.Fatal("expected stale --confirm-remote on matching-base 1->2 revert to fail preflight")
	}
	if len(api.uploaded) != 0 || len(api.patches) != patchesBefore {
		t.Fatalf("stale --confirm-remote must abort before uploads or permission narrowing: uploaded=%v patches=%d", api.uploaded, len(api.patches)-patchesBefore)
	}

	dir2 := t.TempDir()
	doc2 := setupHTMLDoc(t, dir2, "---\ntitle: S4\n---\n\nStart\n")
	api2 := newWriteFakeAPI()
	if _, err := sync.Push(context.Background(), doc2, api2, cfg, sync.PushOptions{WorkDir: dir2}, &out, &errBuf); err != nil {
		t.Fatalf("initial md push: %v", err)
	}
	migDoc := strings.Replace(mustRead(t, doc2), "title: S4\n", "title: S4\nformat: html\n", 1)
	if err := os.WriteFile(doc2, []byte(migDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	api2.failVerifyOnce = true
	if _, err := sync.Push(context.Background(), doc2, api2, cfg, sync.PushOptions{WorkDir: dir2}, &out, &errBuf); err == nil {
		t.Fatal("expected lost-response error on 2->1 migration")
	}
	out.Reset()
	errBuf.Reset()
	if _, err := sync.Status(context.Background(), doc2, api2, cfg, "", &out, &errBuf); err != nil {
		t.Fatalf("Status after lost-response 2->1 migration: %v", err)
	}
	if !strings.Contains(out.String(), `remote: changed (use "elport diff")`) || strings.Contains(out.String(), `use "elport pull"`) {
		t.Fatalf("status after lost-response 2->1 migration must steer to diff, not pull:\n%s", out.String())
	}
}
