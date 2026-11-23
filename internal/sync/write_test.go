package sync_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/youthesame/elport/internal/config"
	"github.com/youthesame/elport/internal/frontmatter"
	"github.com/youthesame/elport/internal/plan"
	"github.com/youthesame/elport/internal/state"
	"github.com/youthesame/elport/internal/sync"
)

type writeFakeAPI struct {
	root       string
	me         map[string]any
	info       map[string]any
	entities   map[string]map[string]any
	getQueue   []map[string]any
	uploads    map[string][]map[string]any
	downloads  map[int][]byte
	categories map[string][]map[string]any
	statuses   map[string][]map[string]any

	calls        []string
	created      [][2]string
	patches      []map[string]any
	uploaded     []string
	addedTags    []string
	addedLinks   [][4]string
	nextID       int
	nextUploadID int

	patchTransform func(payload map[string]any) map[string]any
	failUploadOnce bool
	failPatchOnce  bool
	failVerifyOnce bool
}

func newWriteFakeAPI() *writeFakeAPI {
	return &writeFakeAPI{
		root:         "https://e.example",
		me:           map[string]any{"userid": json.Number("1"), "team": json.Number("7")},
		info:         map[string]any{"elabftw_version": "5.1.0"},
		entities:     map[string]map[string]any{},
		uploads:      map[string][]map[string]any{},
		downloads:    map[int][]byte{},
		categories:   map[string][]map[string]any{},
		statuses:     map[string][]map[string]any{},
		nextID:       42,
		nextUploadID: 100,
	}
}

func (f *writeFakeAPI) Root() string { return f.root }

func (f *writeFakeAPI) Me(context.Context) (map[string]any, error) {
	f.calls = append(f.calls, "me")
	return f.me, nil
}

func (f *writeFakeAPI) Info(context.Context) (map[string]any, error) {
	return f.info, nil
}

func (f *writeFakeAPI) Get(_ context.Context, entity string, eid int) (map[string]any, error) {
	f.calls = append(f.calls, fmt.Sprintf("get:%s/%d", entity, eid))
	if len(f.getQueue) > 0 {
		doc := f.getQueue[0]
		f.getQueue = f.getQueue[1:]
		return doc, nil
	}
	if doc, ok := f.entities[fmt.Sprintf("%s/%d", entity, eid)]; ok {
		return doc, nil
	}
	return map[string]any{"body": "", "content_type": 2, "userid": json.Number("1"), "team": json.Number("7")}, nil
}

func (f *writeFakeAPI) Create(_ context.Context, entity, title string) (map[string]any, error) {
	f.calls = append(f.calls, "create")
	f.created = append(f.created, [2]string{entity, title})
	id := f.nextID
	f.nextID++
	f.entities[fmt.Sprintf("%s/%d", entity, id)] = map[string]any{
		"id":           json.Number(fmt.Sprintf("%d", id)),
		"title":        title,
		"body":         "",
		"content_type": 2,
		"userid":       json.Number("1"),
		"team":         json.Number("7"),
	}
	return map[string]any{"id": json.Number(fmt.Sprintf("%d", id))}, nil
}

func (f *writeFakeAPI) Patch(_ context.Context, entity string, eid int, payload map[string]any) (map[string]any, error) {
	f.calls = append(f.calls, "patch")
	cp := map[string]any{}
	for k, v := range payload {
		cp[k] = v
	}
	f.patches = append(f.patches, cp)
	if f.failPatchOnce {
		f.failPatchOnce = false
		return nil, errors.New("patch failed")
	}
	key := fmt.Sprintf("%s/%d", entity, eid)
	doc := f.entities[key]
	if doc == nil {
		doc = map[string]any{"userid": json.Number("1"), "team": json.Number("7")}
		f.entities[key] = doc
	}
	for k, v := range payload {
		doc[k] = v
	}
	if f.patchTransform != nil {
		doc = f.patchTransform(doc)
		f.entities[key] = doc
	}
	if f.failVerifyOnce {
		f.failVerifyOnce = false
		return nil, errors.New("verify failed")
	}
	return doc, nil
}

func (f *writeFakeAPI) Uploads(_ context.Context, entity string, eid int) ([]map[string]any, error) {
	f.calls = append(f.calls, "uploads")
	return f.uploads[fmt.Sprintf("%s/%d", entity, eid)], nil
}

func (f *writeFakeAPI) Comments(context.Context, string, int) ([]map[string]any, error) {
	return nil, nil
}

func (f *writeFakeAPI) Upload(_ context.Context, entity string, eid int, approved plan.ApprovedUploadPath) (map[string]any, error) {
	f.calls = append(f.calls, "upload")
	if !approved.Valid() {
		return nil, errors.New("invalid approved upload path")
	}
	if f.failUploadOnce {
		f.failUploadOnce = false
		return nil, errors.New("upload failed")
	}
	raw, err := os.ReadFile(approved.Path())
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	uid := f.nextUploadID
	f.nextUploadID++
	name := filepath.Base(approved.Path())
	f.uploaded = append(f.uploaded, name)
	entry := map[string]any{
		"id":             json.Number(fmt.Sprintf("%d", uid)),
		"real_name":      name,
		"long_name":      fmt.Sprintf("ab/%d-%s", uid, name),
		"storage":        1,
		"hash_algorithm": "sha256",
		"hash":           hex.EncodeToString(sum[:]),
	}
	key := fmt.Sprintf("%s/%d", entity, eid)
	f.uploads[key] = append(f.uploads[key], entry)
	f.downloads[uid] = raw
	// Simulate eLabFTW returning only {"id": uid} so completeUploads must fetch metadata.
	return map[string]any{"id": json.Number(fmt.Sprintf("%d", uid))}, nil
}

func (f *writeFakeAPI) Download(_ context.Context, _ string, _ int, uid int) ([]byte, error) {
	return f.downloads[uid], nil
}

func (f *writeFakeAPI) AddTag(_ context.Context, _ string, _ int, tag string) error {
	f.calls = append(f.calls, "add_tag")
	f.addedTags = append(f.addedTags, tag)
	return nil
}

func (f *writeFakeAPI) AddLink(_ context.Context, entity string, eid int, targetEntity string, targetID int) error {
	f.calls = append(f.calls, "add_link")
	f.addedLinks = append(f.addedLinks, [4]string{entity, fmt.Sprintf("%d", eid), targetEntity, fmt.Sprintf("%d", targetID)})
	return nil
}

func (f *writeFakeAPI) AddComment(context.Context, string, int, string) error { return nil }

func (f *writeFakeAPI) Categories(_ context.Context, _ any, entity string) ([]map[string]any, error) {
	return f.categories[entity], nil
}

func (f *writeFakeAPI) Statuses(_ context.Context, _ any, entity string) ([]map[string]any, error) {
	return f.statuses[entity], nil
}

func (f *writeFakeAPI) Search(context.Context, string, url.Values) ([]map[string]any, error) {
	return nil, nil
}

func testConfig() *config.Data {
	return &config.Data{
		Raw: map[string]any{
			"default_profile": "test",
			"profiles": map[string]any{
				"test": map[string]any{
					"base_url":   "https://e.example",
					"verify_ssl": true,
				},
			},
		},
		UserProfiles: map[string]bool{"test": true},
	}
}

func setIsolatedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func TestSidecarPathMatchesPythonStem(t *testing.T) {
	cases := []struct {
		in     string
		remote string
		base   string
	}{
		{"report.md", "report.remote.md", "report.base.md"},
		{filepath.Join("dir", "a.b.md"), filepath.Join("dir", "a.b.remote.md"), filepath.Join("dir", "a.b.base.md")},
		{"plain", "plain.remote.md", "plain.base.md"},
		{".hidden", ".hidden.remote.md", ".hidden.base.md"},
		{".hidden.md", ".hidden.remote.md", ".hidden.base.md"},
	}
	for _, tc := range cases {
		if got := sync.SidecarPath(tc.in); got != tc.remote {
			t.Errorf("SidecarPath(%q) = %q, want %q", tc.in, got, tc.remote)
		}
		if got := sync.BaseSidecarPath(tc.in); got != tc.base {
			t.Errorf("BaseSidecarPath(%q) = %q, want %q", tc.in, got, tc.base)
		}
	}
}

func TestPushCreateUploadReuseAndSameFormBase(t *testing.T) {
	setIsolatedHome(t)
	dir := t.TempDir()
	docPath := filepath.Join(dir, "report.md")
	if err := os.WriteFile(filepath.Join(dir, "fig.png"), []byte("png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(docPath, []byte("---\ntitle: Demo\ntags:\n- alpha\n---\n\nSee ![fig](fig.png)\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	api := newWriteFakeAPI()
	// Server normalizes body differently from sent bytes to verify remote_base comes from PATCH response.
	api.patchTransform = func(doc map[string]any) map[string]any {
		doc["body"] = doc["body"].(string) + "\n<!-- server normalized -->"
		doc["content_type"] = 2
		return doc
	}

	var out, errBuf bytes.Buffer
	summary, err := sync.Push(context.Background(), docPath, api, testConfig(), sync.PushOptions{WorkDir: dir}, &out, &errBuf)
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}
	if !strings.Contains(summary, "1 new upload(s)") {
		t.Fatalf("unexpected summary: %q", summary)
	}
	if len(api.uploaded) != 1 || api.uploaded[0] != "fig.png" {
		t.Fatalf("uploaded = %v, want [fig.png]", api.uploaded)
	}
	if len(api.addedTags) != 1 || api.addedTags[0] != "alpha" {
		t.Fatalf("addedTags = %v, want [alpha]", api.addedTags)
	}

	// Front matter should now have id=42, entity=experiments, profile=test while body stays untouched.
	doc, err := sync.ReadDocument(docPath, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !doc.HasID || doc.ID != 42 {
		t.Fatalf("expected id=42, got %+v", doc)
	}
	if string(doc.Body) != "See ![fig](fig.png)\n" {
		t.Fatalf("local body was mutated: %q", doc.Body)
	}

	saved, err := state.Load("https://e.example", "experiments", "42")
	if err != nil || saved == nil {
		t.Fatalf("state missing: %v", err)
	}
	if !strings.Contains(saved["remote_base"].(string), "server normalized") {
		t.Fatalf("remote_base did not capture server-normalized response: %q", saved["remote_base"])
	}
	if saved["local_base"].(string) != "See ![fig](fig.png)\n" {
		t.Fatalf("local_base mismatch: %q", saved["local_base"])
	}

	// Second push with unchanged body and identical attachment should reuse upload and skip PATCH.
	api.patches = nil
	api.uploaded = nil
	out.Reset()
	summary2, err := sync.Push(context.Background(), docPath, api, testConfig(), sync.PushOptions{WorkDir: dir}, &out, &errBuf)
	if err != nil {
		t.Fatalf("second Push failed: %v", err)
	}
	if len(api.uploaded) != 0 {
		t.Fatalf("expected upload reuse, got new uploads: %v", api.uploaded)
	}
	if len(api.patches) != 0 {
		t.Fatalf("expected unchanged push to skip PATCH, got %d patches", len(api.patches))
	}
	if summary2 != "pushed (body unchanged)" {
		t.Fatalf("unexpected second summary: %q", summary2)
	}
}

func TestPushResumesAfterInterruptedCreate(t *testing.T) {
	for _, stage := range []string{"upload", "patch", "verify"} {
		t.Run(stage, func(t *testing.T) {
			setIsolatedHome(t)
			dir := t.TempDir()
			docPath := filepath.Join(dir, "report.md")
			if err := os.WriteFile(filepath.Join(dir, "data.csv"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(docPath, []byte("---\ntitle: Resume\n---\n\n[d](data.csv)\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			api := newWriteFakeAPI()
			switch stage {
			case "upload":
				api.failUploadOnce = true
			case "patch":
				api.failPatchOnce = true
			case "verify":
				api.failVerifyOnce = true
			}

			var out, errBuf bytes.Buffer
			_, err := sync.Push(context.Background(), docPath, api, testConfig(), sync.PushOptions{WorkDir: dir}, &out, &errBuf)
			if err == nil {
				t.Fatalf("expected failure at %s", stage)
			}
			saved, _ := state.Load("https://e.example", "experiments", "42")
			if saved == nil || saved["pending_create"] != true {
				t.Fatalf("expected pending_create=true, got %v", saved)
			}

			// Plain retry without --force must succeed and not create a duplicate entity.
			out.Reset()
			errBuf.Reset()
			if _, err := sync.Push(context.Background(), docPath, api, testConfig(), sync.PushOptions{WorkDir: dir}, &out, &errBuf); err != nil {
				t.Fatalf("resume Push failed: %v", err)
			}
			if len(api.created) != 1 {
				t.Fatalf("expected 1 create total, got %d", len(api.created))
			}
			saved, _ = state.Load("https://e.example", "experiments", "42")
			if _, stillPending := saved["pending_create"]; stillPending {
				t.Fatalf("pending_create should be cleared after resume: %v", saved)
			}
			if stage == "verify" && !strings.Contains(errBuf.String(), "resuming an interrupted creation") {
				t.Fatalf("expected resume warning when remote body is non-empty, stderr=%q", errBuf.String())
			}
		})
	}
}

func TestPushRefusesLockEvenWithForceAndRefusesHTML(t *testing.T) {
	setIsolatedHome(t)
	dir := t.TempDir()
	docPath := filepath.Join(dir, "report.md")
	if err := os.WriteFile(docPath, []byte("---\nid: 1\nentity: experiments\nprofile: test\n---\n\nlocal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.Save("https://e.example", "experiments", "1", map[string]any{
		"remote_base": "old",
		"local_base":  "old",
	}); err != nil {
		t.Fatal(err)
	}

	api := newWriteFakeAPI()
	api.entities["experiments/1"] = map[string]any{"body": "old", "content_type": 2, "locked": 1}
	var out, errBuf bytes.Buffer
	if _, err := sync.Push(context.Background(), docPath, api, testConfig(), sync.PushOptions{Force: true, WorkDir: dir}, &out, &errBuf); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("expected lock refusal even with --force, got: %v", err)
	}

	api.entities["experiments/1"] = map[string]any{"body": "<p>rich text</p>", "content_type": 1}
	if _, err := sync.Push(context.Background(), docPath, api, testConfig(), sync.PushOptions{Force: true, WorkDir: dir}, &out, &errBuf); err == nil || !strings.Contains(err.Error(), "HTML entity") {
		t.Fatalf("expected HTML refusal, got: %v", err)
	}
}

func TestPushPermissionsWideningAndNarrowingOrder(t *testing.T) {
	setIsolatedHome(t)
	dir := t.TempDir()
	docPath := filepath.Join(dir, "report.md")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(docPath, []byte("---\nid: 5\nentity: experiments\nprofile: test\nread: owner\nwrite: account\n---\n\n[a](a.txt)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.Save("https://e.example", "experiments", "5", map[string]any{
		"remote_base": "old",
		"local_base":  "old",
		"meta_base":   map[string]any{"read": "team", "write": "team"},
	}); err != nil {
		t.Fatal(err)
	}

	api := newWriteFakeAPI()
	api.entities["experiments/5"] = map[string]any{
		"body":          "old",
		"content_type":  2,
		"userid":        json.Number("1"),
		"team":          json.Number("7"),
		"canread_base":  30,
		"canwrite_base": 30,
		"canread":       `{"base":30,"teams":[2],"users":[],"teamgroups":[]}`,
	}

	var out, errBuf bytes.Buffer
	// Without --yes and without TTY, widening write->account must fail before any PATCH or upload.
	if _, err := sync.Push(context.Background(), docPath, api, testConfig(), sync.PushOptions{WorkDir: dir}, &out, &errBuf); err == nil || !strings.Contains(err.Error(), "permission widening requires confirmation") {
		t.Fatalf("expected widening confirmation error, got: %v", err)
	}
	if len(api.patches) != 0 || len(api.uploaded) != 0 {
		t.Fatalf("expected no writes before confirmation, patches=%d uploaded=%d", len(api.patches), len(api.uploaded))
	}

	// With --yes, narrowing (read->owner) must PATCH BEFORE upload, and individual grants must warn.
	api.calls = nil
	if _, err := sync.Push(context.Background(), docPath, api, testConfig(), sync.PushOptions{AssumeYes: true, WorkDir: dir}, &out, &errBuf); err != nil {
		t.Fatalf("Push with --yes failed: %v", err)
	}
	firstPatch, firstUpload := -1, -1
	for i, c := range api.calls {
		if c == "patch" && firstPatch < 0 {
			firstPatch = i
		}
		if c == "upload" && firstUpload < 0 {
			firstUpload = i
		}
	}
	if firstPatch < 0 || firstUpload < 0 || firstPatch >= firstUpload {
		t.Fatalf("expected narrowing patch before upload, calls=%v", api.calls)
	}
	if !strings.Contains(errBuf.String(), "individual grants remain") {
		t.Fatalf("expected individual grants warning, stderr=%q", errBuf.String())
	}
}

func TestPushDocumentLinksAndRemoteLinkedExclusion(t *testing.T) {
	home := setIsolatedHome(t)
	cfgDir := filepath.Join(home, ".config", "elport")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte("default_profile = \"test\"\n\n[profiles.test]\nbase_url = \"https://e.example\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	otherPath := filepath.Join(dir, "other.md")
	if err := os.WriteFile(otherPath, []byte("---\nid: 12\nentity: items\nprofile: test\n---\n\nother\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.csv"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	docPath := filepath.Join(dir, "report.md")
	if err := os.WriteFile(docPath, []byte("---\nid: 13\nentity: experiments\nprofile: test\n---\n\n[o](other.md) [s](secret.csv)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Remote body already contained a bare path to secret.csv (written by someone else).
	if err := state.Save("https://e.example", "experiments", "13", map[string]any{
		"remote_base": "[s](secret.csv)",
		"local_base":  "old",
	}); err != nil {
		t.Fatal(err)
	}

	api := newWriteFakeAPI()
	api.entities["experiments/13"] = map[string]any{"body": "[s](secret.csv)", "content_type": 2, "userid": 1, "team": 7}

	var out, errBuf bytes.Buffer
	if _, err := sync.Push(context.Background(), docPath, api, testConfig(), sync.PushOptions{WorkDir: dir}, &out, &errBuf); err != nil {
		t.Fatalf("Push failed: %v", err)
	}
	// secret.csv must NOT be uploaded; other.md must be rewritten to database.php URL and linked once.
	if len(api.uploaded) != 0 {
		t.Fatalf("remote-linked file was uploaded: %v", api.uploaded)
	}
	if !strings.Contains(errBuf.String(), "linked from the remote body") {
		t.Fatalf("expected remote-linked warning, stderr=%q", errBuf.String())
	}
	sentBody := api.patches[0]["body"].(string)
	if !strings.Contains(sentBody, "https://e.example/database.php?mode=view&id=12") {
		t.Fatalf("doc link not rewritten: %q", sentBody)
	}
	if len(api.addedLinks) != 1 || api.addedLinks[0] != [4]string{"experiments", "13", "items", "12"} {
		t.Fatalf("addedLinks = %v", api.addedLinks)
	}
}

func TestPullConflictAndGitMergeFlow(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	setIsolatedHome(t)
	dir := t.TempDir()
	docPath := filepath.Join(dir, "report.md")
	baseBody := "first\nmiddle\nlast\n"
	localBody := "local first\nmiddle\nlast\n"
	remoteBody := "first\nmiddle\nremote last\n"

	if err := os.WriteFile(docPath, []byte("---\nid: 7\nentity: experiments\nprofile: test\n---\n\n"+localBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.Save("https://e.example", "experiments", "7", map[string]any{
		"remote_base": baseBody,
		"local_base":  baseBody,
	}); err != nil {
		t.Fatal(err)
	}

	api := newWriteFakeAPI()
	api.entities["experiments/7"] = map[string]any{"body": remoteBody, "content_type": 2, "userid": 1, "team": 7}

	var out, errBuf bytes.Buffer
	_, err := sync.Pull(context.Background(), docPath, api, testConfig(), "", &out, &errBuf)
	if err == nil || !strings.Contains(err.Error(), "local changes conflict") {
		t.Fatalf("expected pull conflict, got: %v", err)
	}
	if _, err := os.Stat(sync.BaseSidecarPath(docPath)); err != nil {
		t.Fatalf("missing .base.md sidecar: %v", err)
	}
	if _, err := os.Stat(sync.SidecarPath(docPath)); err != nil {
		t.Fatalf("missing .remote.md sidecar: %v", err)
	}

	// Verify CRLF in a sidecar is normalized cleanly before git merge-file.
	rawRemote, _ := os.ReadFile(sync.SidecarPath(docPath))
	if err := os.WriteFile(sync.SidecarPath(docPath), bytes.ReplaceAll(rawRemote, []byte("\n"), []byte("\r\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if err := sync.Merge(context.Background(), docPath, testConfig(), "", false, &out); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}
	if !strings.Contains(out.String(), "merged cleanly") {
		t.Fatalf("unexpected merge output: %q", out.String())
	}
	if _, err := os.Stat(sync.BaseSidecarPath(docPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected .base.md removed after merge")
	}
	if _, err := os.Stat(sync.SidecarPath(docPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected .remote.md removed after merge")
	}

	rawMerged, _ := os.ReadFile(docPath)
	_, mergedBody, err := frontmatter.Parse(string(rawMerged))
	if err != nil {
		t.Fatal(err)
	}
	if mergedBody != "local first\nmiddle\nremote last\n" {
		t.Fatalf("mergedBody = %q", mergedBody)
	}

	// Promoted base allows regular push without --force.
	out.Reset()
	if _, err := sync.Push(context.Background(), docPath, api, testConfig(), sync.PushOptions{WorkDir: dir}, &out, &errBuf); err != nil {
		t.Fatalf("regular push after merge failed: %v", err)
	}
	if len(api.patches) != 1 || api.patches[0]["body"] != "local first\nmiddle\nremote last\n" {
		t.Fatalf("unexpected patch after merge: %+v", api.patches)
	}
}

func TestPullMetadataMergeAndNoBaseConflict(t *testing.T) {
	setIsolatedHome(t)
	dir := t.TempDir()
	docPath := filepath.Join(dir, "report.md")
	if err := os.WriteFile(docPath, []byte("---\nid: 9\nentity: experiments\nprofile: test\ntitle: Local Edit\ncategory: Old Cat\ntags:\n- local\n---\n\nlocal body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.Save("https://e.example", "experiments", "9", map[string]any{
		"remote_base": "base body\n",
		"local_base":  "base body\n",
		"meta_base":   map[string]any{"title": "Old Title", "category": "Old Cat"},
	}); err != nil {
		t.Fatal(err)
	}

	api := newWriteFakeAPI()
	api.entities["experiments/9"] = map[string]any{
		"body":           "base body\n",
		"title":          "Web Title",
		"category_title": "Web Cat",
		"tags":           "web|shared",
		"content_type":   2,
	}

	var out, errBuf bytes.Buffer
	summary, err := sync.Pull(context.Background(), docPath, api, testConfig(), "", &out, &errBuf)
	if err != nil {
		t.Fatalf("metadata-only Pull failed: %v", err)
	}
	if summary != sync.UpToDate {
		t.Fatalf("summary = %q, want %q", summary, sync.UpToDate)
	}
	doc, err := sync.ReadDocument(docPath, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	// Local title edit preserved, Web category taken, tags unioned.
	if doc.Meta.Values["title"] != "Local Edit" {
		t.Fatalf("local title edit lost: %v", doc.Meta.Values["title"])
	}
	if doc.Meta.Values["category"] != "Web Cat" {
		t.Fatalf("remote category not pulled: %v", doc.Meta.Values["category"])
	}
	if string(doc.Body) != "local body\n" {
		t.Fatalf("local dirty body changed: %q", doc.Body)
	}

	// No-base conflict on a different document writes .remote.md without overwriting local doc.
	doc2 := filepath.Join(dir, "other.md")
	if err := os.WriteFile(doc2, []byte("---\nid: 10\nentity: experiments\nprofile: test\n---\n\nmy local notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	api.entities["experiments/10"] = map[string]any{"body": "server text\n", "content_type": 2}
	_, err = sync.Pull(context.Background(), doc2, api, testConfig(), "", &out, &errBuf)
	if err == nil || !strings.Contains(err.Error(), "base unavailable; remote written to other.remote.md") {
		t.Fatalf("expected no-base conflict error, got: %v", err)
	}
	sidecarBytes, err := os.ReadFile(filepath.Join(dir, "other.remote.md"))
	if err != nil || string(sidecarBytes) != "server text\n" {
		t.Fatalf("unexpected sidecar: %q err=%v", sidecarBytes, err)
	}
}

func TestMergeResolvedWithoutGitAndRefusesAttachmentConflict(t *testing.T) {
	setIsolatedHome(t)
	dir := t.TempDir()
	docPath := filepath.Join(dir, "report.md")
	if err := os.WriteFile(docPath, []byte("---\nid: 11\nentity: experiments\nprofile: test\n---\n\nresolved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sync.BaseSidecarPath(docPath), []byte("---\nid: 11\nentity: experiments\nprofile: test\n---\n\nbase\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sync.SidecarPath(docPath), []byte("---\nid: 11\nentity: experiments\nprofile: test\n---\n\n[img](fig.png)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fig.png.remote"), []byte("remote img"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.Save("https://e.example", "experiments", "11", map[string]any{
		"remote_base":    "base\n",
		"local_base":     "base\n",
		"pending_remote": "[img](fig.png)\n",
	}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := sync.Merge(context.Background(), docPath, testConfig(), "", false, &out); err == nil || !strings.Contains(err.Error(), "attachment conflicts remain: fig.png.remote") {
		t.Fatalf("expected attachment conflict refusal, got: %v", err)
	}

	t.Setenv("PATH", "")
	if err := sync.Merge(context.Background(), docPath, testConfig(), "", true, &out); err != nil {
		t.Fatalf("Merge --resolved without git failed: %v", err)
	}
	saved, _ := state.Load("https://e.example", "experiments", "11")
	if saved["remote_base"] != "[img](fig.png)\n" || saved["local_base"] != "[img](fig.png)\n" {
		t.Fatalf("pending_remote not promoted: %v", saved)
	}
	if _, hasPending := saved["pending_remote"]; hasPending {
		t.Fatalf("pending_remote should be cleared: %v", saved)
	}
}
