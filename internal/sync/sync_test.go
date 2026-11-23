package sync_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/youthesame/elport/internal/client"
	"github.com/youthesame/elport/internal/config"
	"github.com/youthesame/elport/internal/plan"
	"github.com/youthesame/elport/internal/state"
	"github.com/youthesame/elport/internal/sync"
)

type fakeAPI struct {
	root       string
	entities   map[string]map[string]any
	uploads    map[string][]map[string]any
	downloads  map[int][]byte
	comments   map[string][]map[string]any
	posted     []string
	info       map[string]any
	getErr     error
	uploadsErr error
}

func (f *fakeAPI) Root() string                                                   { return f.root }
func (f *fakeAPI) Me(context.Context) (map[string]any, error)                     { return nil, nil }
func (f *fakeAPI) Info(context.Context) (map[string]any, error)                   { return f.info, nil }
func (f *fakeAPI) Create(context.Context, string, string) (map[string]any, error) { return nil, nil }
func (f *fakeAPI) Patch(context.Context, string, int, map[string]any) (map[string]any, error) {
	return nil, nil
}
func (f *fakeAPI) Upload(context.Context, string, int, plan.ApprovedUploadPath) (map[string]any, error) {
	return nil, nil
}
func (f *fakeAPI) AddTag(context.Context, string, int, string) error       { return nil }
func (f *fakeAPI) AddLink(context.Context, string, int, string, int) error { return nil }
func (f *fakeAPI) Categories(context.Context, any, string) ([]map[string]any, error) {
	return nil, nil
}
func (f *fakeAPI) Statuses(context.Context, any, string) ([]map[string]any, error) {
	return nil, nil
}
func (f *fakeAPI) Search(context.Context, string, url.Values) ([]map[string]any, error) {
	return nil, nil
}

func keyFor(entity string, eid int) string {
	return entity + "/" + json.Number(string(rune('0'+eid))).String()
}

func entKey(entity string, eid int) string {
	return entity + ":" + string(rune(eid))
}

func (f *fakeAPI) Get(_ context.Context, entity string, eid int) (map[string]any, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	doc, ok := f.entities[entKey(entity, eid)]
	if !ok {
		return nil, client.NotFoundError{Msg: "API 404: missing"}
	}
	return doc, nil
}

func (f *fakeAPI) Uploads(_ context.Context, entity string, eid int) ([]map[string]any, error) {
	if f.uploadsErr != nil {
		return nil, f.uploadsErr
	}
	return f.uploads[entKey(entity, eid)], nil
}

func (f *fakeAPI) Comments(_ context.Context, entity string, eid int) ([]map[string]any, error) {
	return f.comments[entKey(entity, eid)], nil
}

func (f *fakeAPI) Download(_ context.Context, _ string, _ int, uid int) ([]byte, error) {
	return f.downloads[uid], nil
}

func (f *fakeAPI) AddComment(_ context.Context, entity string, eid int, text string) error {
	f.posted = append(f.posted, text)
	return nil
}

func TestParseEntityURLStrictValidation(t *testing.T) {
	const base = "https://elab.example.test:50011"
	entity, id, err := sync.ParseEntityURL("https://elab.example.test:50011/experiments.php?mode=view&id=357", base)
	if err != nil || entity != "experiments" || id != 357 {
		t.Fatalf("valid experiments URL failed: (%q, %d, %v)", entity, id, err)
	}
	entity, id, err = sync.ParseEntityURL("https://elab.example.test:50011/database.php?id=12&mode=view", base)
	if err != nil || entity != "items" || id != 12 {
		t.Fatalf("valid items URL failed: (%q, %d, %v)", entity, id, err)
	}
	for _, bad := range []string{
		"http://elab.example.test:50011/experiments.php?mode=view&id=1",
		"https://user:pass@elab.example.test:50011/experiments.php?mode=view&id=1",
		"https://evil.example.test:50011/experiments.php?mode=view&id=1",
		"https://elab.example.test/experiments.php?mode=view&id=1",
		"https://elab.example.test:50011/experiments.php?mode=edit&id=1",
		"https://elab.example.test:50011/experiments.php?mode=view&id=1&extra=2",
		"https://elab.example.test:50011/experiments.php?mode=view&id=0",
		"https://elab.example.test:50011/other.php?mode=view&id=1",
	} {
		if _, _, err := sync.ParseEntityURL(bad, base); err == nil {
			t.Errorf("expected invalid entity URL %q to be rejected", bad)
		}
	}
}

func TestStatusDiffFetchViewSnapshotAndComments(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ELABFTW_BASE_URL", "")
	t.Setenv("ELABFTW_API_KEY", "")

	const baseURL = "https://elab.example.test"
	docDir := t.TempDir()
	docPath := filepath.Join(docDir, "report.md")
	figPath := filepath.Join(docDir, "fig.png")
	figBytes := []byte("PNGDATA")
	if err := os.WriteFile(figPath, figBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(figBytes)
	figHash := hex.EncodeToString(sum[:])

	if err := os.WriteFile(docPath, []byte("---\nid: 5\nentity: experiments\nprofile: lab\nread: team\n---\n\n![fig](fig.png)\n<figure></figure>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(baseURL, "experiments", "5", map[string]any{
		"local_base":  "![fig](fig.png)\n",
		"remote_base": "![fig](https://elab.example.test/app/download.php?f=abc/1.png&name=fig.png&storage=1)\n",
		"path":        docPath,
	}); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Data{
		Raw: map[string]any{
			"default_profile": "lab",
			"profiles": map[string]any{
				"lab": map[string]any{"base_url": baseURL, "verify_ssl": true},
			},
		},
		UserProfiles: map[string]bool{"lab": true},
	}

	api := &fakeAPI{
		root: baseURL,
		info: map[string]any{"elabftw_version": "6.0.9"},
		entities: map[string]map[string]any{
			entKey("experiments", 5): {
				"id":           5,
				"content_type": 2,
				"locked":       1,
				"canread_base": 10,
				"body":         "![fig](https://elab.example.test/app/download.php?f=abc/1.png&name=fig.png&storage=1)\n",
				"uploads": []any{
					map[string]any{
						"id":             10,
						"real_name":      "fig.png",
						"long_name":      "abc/1.png",
						"storage":        1,
						"hash_algorithm": "sha256",
						"hash":           figHash,
					},
					map[string]any{
						"id":        11,
						"real_name": "extra.csv",
						"long_name": "def/2.csv",
						"storage":   1,
					},
				},
				"steps": []any{
					map[string]any{"ordering": 2, "finished": 1, "body": "Second step"},
					map[string]any{"ordering": 1, "finished": 0, "body": "First step"},
				},
				"experiments_links": []any{
					map[string]any{"entityid": 9, "title": "Related Exp"},
				},
			},
		},
		uploads: map[string][]map[string]any{
			entKey("experiments", 5): {
				{"id": 10, "real_name": "fig.png", "long_name": "abc/1.png", "storage": 1, "hash_algorithm": "sha256", "hash": figHash},
				{"id": 11, "real_name": "extra.csv", "long_name": "def/2.csv", "storage": 1},
			},
		},
		downloads: map[int][]byte{
			10: figBytes,
			11: []byte("a,b\n1,2\n"),
		},
		comments: map[string][]map[string]any{
			entKey("experiments", 5): {
				{"fullname": "Ada", "created_at": "2026-11-23 10:00", "modified_at": "2026-11-23 10:00", "comment": "Looks good"},
			},
		},
	}

	var out, errBuf bytes.Buffer
	summary, err := sync.Status(context.Background(), docPath, api, cfg, "", &out, &errBuf)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	for _, want := range []string{"local: dirty", "uploads reuse: fig.png", "mode: markdown", "lock: locked", "permissions: read owner→team", "remote: unchanged"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("Status stdout missing %q:\n%s", want, out.String())
		}
	}
	if !strings.Contains(errBuf.String(), "eLabFTW 6.x strips raw HTML") {
		t.Errorf("expected 6.x raw HTML warning on stderr, got %q", errBuf.String())
	}
	if !strings.Contains(summary, "locked") || !strings.Contains(summary, "local dirty") {
		t.Errorf("unexpected summary: %q", summary)
	}

	out.Reset()
	errBuf.Reset()
	if err := sync.Diff(context.Background(), docPath, api, cfg, "", false, &out, &errBuf); err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(out.String(), "+<figure></figure>") || !strings.Contains(errBuf.String(), "normalization noise") {
		t.Errorf("unexpected Diff output: stdout=%q stderr=%q", out.String(), errBuf.String())
	}

	out.Reset()
	errBuf.Reset()
	if _, err := sync.Fetch(context.Background(), docPath, api, cfg, "", &out, &errBuf); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(docDir, "extra.csv")); err != nil || string(got) != "a,b\n1,2\n" {
		t.Fatalf("Fetch did not write extra.csv: (%q, %v)", string(got), err)
	}

	out.Reset()
	errBuf.Reset()
	if err := sync.View(context.Background(), api, "experiments", 5, baseURL, "", &out, &errBuf); err != nil {
		t.Fatalf("View stdout: %v", err)
	}
	if out.String() != "![fig](fig.png)\n" {
		t.Errorf("View stdout = %q, want %q", out.String(), "![fig](fig.png)\n")
	}
	step1Idx := strings.Index(errBuf.String(), "step [ ] First step")
	step2Idx := strings.Index(errBuf.String(), "step [x] Second step")
	if step1Idx < 0 || step2Idx < 0 || step1Idx > step2Idx {
		t.Errorf("steps missing or out of order on stderr:\n%s", errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "links to experiments/9: Related Exp") {
		t.Errorf("missing link on stderr:\n%s", errBuf.String())
	}

	snapDir := filepath.Join(t.TempDir(), "snap")
	out.Reset()
	errBuf.Reset()
	if err := sync.View(context.Background(), api, "experiments", 5, baseURL, snapDir, &out, &errBuf); err != nil {
		t.Fatalf("View -o snapshot: %v", err)
	}
	if _, err := os.Stat(filepath.Join(snapDir, "report.md")); err != nil {
		t.Errorf("snapshot missing report.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(snapDir, "extra.csv")); err != nil {
		t.Errorf("snapshot missing extra.csv: %v", err)
	}

	out.Reset()
	if err := sync.Comments(context.Background(), docPath, api, cfg, &out); err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if !strings.Contains(out.String(), "Ada 2026-11-23 10:00\nLooks good") {
		t.Errorf("unexpected Comments output: %q", out.String())
	}

	out.Reset()
	if err := sync.Comment(context.Background(), docPath, api, cfg, "New reply", &out); err != nil {
		t.Fatalf("Comment: %v", err)
	}
	if len(api.posted) != 1 || api.posted[0] != "New reply" || !strings.Contains(out.String(), "commented on experiments/5") {
		t.Errorf("unexpected Comment result: posted=%v stdout=%q", api.posted, out.String())
	}

	if runtime.GOOS != "windows" {
		symDir := t.TempDir()
		_ = os.Symlink("/etc/passwd", filepath.Join(symDir, "extra.csv"))
		remote := &sync.Remote{API: api, Entity: "experiments", ID: 5, BaseURL: baseURL}
		uploads, _ := remote.Uploads(context.Background(), nil)
		if _, err := sync.PlaceAttachments(context.Background(), symDir, remote, uploads, ".remote", &errBuf); err == nil {
			t.Fatal("expected PlaceAttachments to refuse symlink destination")
		}
	}
}
