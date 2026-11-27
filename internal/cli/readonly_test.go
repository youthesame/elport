package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	gosync "sync"
	"testing"

	"github.com/youthesame/elport/internal/cli"
	"github.com/youthesame/elport/internal/client"
	"github.com/youthesame/elport/internal/state"
)

func TestMain(m *testing.M) {
	_ = os.Unsetenv("ELPORT_READ_ONLY")
	m.Run()
}

type fakeELab struct {
	t        *testing.T
	srv      *httptest.Server
	mu       gosync.Mutex
	entities map[string]map[string]any
	raw      map[string]string
	requests []string
}

func newFakeELab(t *testing.T) *fakeELab {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ELPORT_READ_ONLY", "")
	f := &fakeELab{t: t, entities: map[string]map[string]any{}, raw: map[string]string{}}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	t.Setenv("ELABFTW_BASE_URL", f.srv.URL)
	t.Setenv("ELABFTW_API_KEY", "12-test-key")
	return f
}

func (f *fakeELab) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	path := strings.TrimPrefix(r.URL.Path, "/api/v2/")
	if body, ok := f.raw[r.Method+" "+path]; ok {
		_, _ = w.Write([]byte(body))
		return
	}
	switch {
	case r.Method == http.MethodGet && path == "users/me":
		_, _ = w.Write([]byte(`{"userid":7,"fullname":"Ada","team":3}`))
	case r.Method == http.MethodGet && path == "info":
		_, _ = w.Write([]byte(`{"elabftw_version":"6.0.9"}`))
	case r.Method == http.MethodGet && path == "apikeys":
		_, _ = w.Write([]byte(`[{"id":12,"can_write":1}]`))
	case r.Method == http.MethodGet && (path == "experiments" || path == "items"):
		_, _ = w.Write([]byte(`[]`))
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/uploads"):
		_, _ = w.Write([]byte(`[]`))
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/comments"):
		_, _ = w.Write([]byte(`[]`))
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/revisions"):
		_, _ = w.Write([]byte(`[]`))
	case r.Method == http.MethodGet:
		if doc, ok := f.entities[path]; ok {
			_ = json.NewEncoder(w).Encode(doc)
			return
		}
		http.NotFound(w, r)
	case r.Method == http.MethodPatch:
		doc, ok := f.entities[path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		var patch map[string]any
		_ = json.NewDecoder(r.Body).Decode(&patch)
		for k, v := range patch {
			doc[k] = v
		}
		_ = json.NewEncoder(w).Encode(doc)
	default:
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":99}`))
	}
}

func (f *fakeELab) env(work string, tty bool) cli.Env {
	return cli.Env{
		WorkDir:    work,
		Keyring:    &memKeyring{},
		ClientOpts: []client.Option{client.WithRootCAs(f.srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs)},
		IsTTY:      &tty,
	}
}

func (f *fakeELab) reset() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	got := f.requests
	f.requests = nil
	return got
}

func (f *fakeELab) writes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, r := range f.requests {
		if !strings.HasPrefix(r, "GET ") {
			out = append(out, r)
		}
	}
	return out
}

func (f *fakeELab) addSyncedDoc(t *testing.T, dir string, id int, local string) string {
	t.Helper()
	idStr := strings.TrimSpace(string(mustJSON(t, id)))
	f.entities["experiments/"+idStr] = map[string]any{"id": id, "title": "Doc " + idStr, "content_type": 2, "body": "same\n", "userid": 7, "team": 3, "uploads": []any{}}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := filepath.Join(dir, "report.md")
	if err := os.WriteFile(doc, []byte("---\nid: "+idStr+"\nentity: experiments\nprofile: lab\ntitle: Doc "+idStr+"\n---\n\n"+local), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(f.srv.URL, "experiments", idStr, map[string]any{
		"remote_base": "same\n",
		"local_base":  "same\n",
		"meta_base":   map[string]any{"title": "Doc " + idStr},
	}); err != nil {
		t.Fatal(err)
	}
	return doc
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func run(env cli.Env, args ...string) (int, string, string) {
	var out, errBuf bytes.Buffer
	code := cli.RunWithEnv(context.Background(), args, nil, &out, &errBuf, env)
	return code, out.String(), errBuf.String()
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestReadOnlyRefusesRemoteWritesWithoutLocalSideEffects(t *testing.T) {
	f := newFakeELab(t)
	work := t.TempDir()
	env := f.env(work, false)
	docDir := filepath.Join(work, "exp")
	doc := f.addSyncedDoc(t, docDir, 42, "edited locally\n")
	before, _ := os.ReadFile(doc)
	t.Setenv("ELPORT_READ_ONLY", "1")

	newDoc := filepath.Join(work, "new.md")
	for _, args := range [][]string{
		{"new", "-o", newDoc, "Title"},
		{"push", doc},
		{"push", "--force", doc},
		{"comment", doc, "hello"},
	} {
		code, _, stderr := run(env, args...)
		if code != cli.ExitError || !strings.Contains(stderr, "read-only mode is enabled (ELPORT_READ_ONLY)") {
			t.Errorf("%v: want read-only refusal, got code=%d stderr=%q", args, code, stderr)
		}
	}
	if got := f.reset(); len(got) != 0 {
		t.Fatalf("refused commands must not reach the server, got %v", got)
	}
	if _, err := os.Stat(newDoc); !os.IsNotExist(err) {
		t.Fatalf("new must not create a document in read-only mode: %v", err)
	}
	if after, _ := os.ReadFile(doc); !bytes.Equal(before, after) {
		t.Fatal("push must not touch the document in read-only mode")
	}
	if names := dirNames(t, docDir); len(names) != 1 {
		t.Fatalf("push must leave no sidecars, got %v", names)
	}

	code, _, stderr := run(env, "comment", "42", "hello")
	if code != cli.ExitError || !strings.Contains(stderr, "refusing POST experiments/42/comments: read-only mode is enabled") {
		t.Fatalf("remote comment: code=%d stderr=%q", code, stderr)
	}

	for _, args := range [][]string{
		{"push", "-n", doc},
		{"status", doc},
		{"diff", doc},
		{"view", "42"},
		{"view", "--json", "42"},
		{"list"},
		{"comments", "42"},
		{"revisions", doc},
		{"revisions", "42"},
	} {
		if code, _, stderr := run(env, args...); code != cli.ExitOK {
			t.Errorf("%v must stay allowed in read-only mode: code=%d stderr=%q", args, code, stderr)
		}
	}
	code, stdout, stderr := run(env, "whoami")
	if code != cli.ExitOK || !strings.Contains(stdout, "  API key   read-write\n") || !strings.Contains(stdout, "  Mode      read-only (ELPORT_READ_ONLY)\n") {
		t.Fatalf("whoami: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if w := f.writes(); len(w) != 0 {
		t.Fatalf("read-only mode sent writes: %v", w)
	}
	if names := dirNames(t, docDir); len(names) != 1 {
		t.Fatalf("allowed commands must not add sidecars here, got %v", names)
	}

	t.Setenv("ELPORT_READ_ONLY", "")
	code, stdout, _ = run(env, "whoami")
	if code != cli.ExitOK || strings.Contains(stdout, "Mode") {
		t.Fatalf("writable whoami must not print a Mode line: %q", stdout)
	}
}

func TestReadOnlyBatchPushIsolatesDirectories(t *testing.T) {
	f := newFakeELab(t)
	work := t.TempDir()
	env := f.env(work, false)
	lockedDir := filepath.Join(work, "a")
	f.addSyncedDoc(t, lockedDir, 42, "changed 42\n")
	f.addSyncedDoc(t, filepath.Join(work, "b"), 43, "changed 43\n")
	lockCfg := filepath.Join(lockedDir, ".elport.toml")
	if err := os.WriteFile(lockCfg, []byte("read_only = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := run(env, "push", work)
	if code != cli.ExitError {
		t.Fatalf("batch push with a refused document must exit 1: stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.Contains(stdout, "✗ "+filepath.Join(work, "a", "report.md")) || !strings.Contains(stdout, "read-only mode is enabled ("+lockCfg+")") {
		t.Fatalf("locked document must be refused with its source: %q %q", stdout, stderr)
	}
	if !strings.Contains(stdout, "✓ "+filepath.Join(work, "b", "report.md")) {
		t.Fatalf("sibling document must push: %q", stdout)
	}
	for _, r := range f.reset() {
		if strings.Contains(r, "experiments/42") {
			t.Fatalf("refused document reached the server: %s", r)
		}
	}
	if f.entities["experiments/43"]["body"] != "changed 43\n" {
		t.Fatalf("sibling PATCH missing: %#v", f.entities["experiments/43"]["body"])
	}
	if names := dirNames(t, lockedDir); len(names) != 2 {
		t.Fatalf("refused document must leave no sidecars, got %v", names)
	}
}
