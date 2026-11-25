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
	"testing"

	"github.com/youthesame/elport/internal/cli"
	"github.com/youthesame/elport/internal/client"
	"github.com/youthesame/elport/internal/frontmatter"
	"github.com/youthesame/elport/internal/state"
)

func TestDirectoryNameMatchesPython(t *testing.T) {
	cases := []struct {
		title string
		want  string
	}{
		{"CRISPR滴定実験", "42-CRISPR滴定実験"},
		{`a/b\c:d*e?f"g<h>i|j`, "42-a_b_c_d_e_f_g_h_i_j"},
		{"..hidden ", "42-hidden"},
		{"", "42"},
		{strings.Repeat("x", 80), "42-" + strings.Repeat("x", 50)},
		{"ｶﾞ", "42-ｶﾞ"},
		{"カ\u3099", "42-ガ"},
	}
	for _, tc := range cases {
		if got := cli.DirectoryName(42, tc.title); got != tc.want {
			t.Errorf("DirectoryName(42, %q) = %q, want %q", tc.title, got, tc.want)
		}
	}
}

func TestCLINewCloneAllAndPushPullMergeFlow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	entities := map[string]map[string]any{
		"experiments/10": {
			"id":             10,
			"title":          "Markdown Note",
			"body":           "hello markdown\n",
			"content_type":   2,
			"category_title": "Assay",
			"tags":           "alpha|beta",
			"canread_base":   30,
			"canwrite_base":  10,
			"userid":         1,
			"team":           7,
		},
		"experiments/11": {
			"id":           11,
			"title":        "HTML Note",
			"body":         "<p>Rich text</p>",
			"content_type": 1,
			"userid":       1,
			"team":         7,
		},
	}
	nextID := 50
	failNextGet := false

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/users/me":
			_ = json.NewEncoder(w).Encode(map[string]any{"userid": 1, "team": 7})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/info":
			_ = json.NewEncoder(w).Encode(map[string]any{"elabftw_version": "5.1.0"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/experiments":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			id := nextID
			nextID++
			key := "experiments/" + strconvItoa(id)
			entities[key] = map[string]any{
				"id":           id,
				"title":        body["title"],
				"body":         "",
				"content_type": 2,
				"userid":       1,
				"team":         7,
				"sharelink":    "https://example.invalid/e/" + strconvItoa(id),
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/experiments":
			if r.URL.Query().Get("offset") != "" && r.URL.Query().Get("offset") != "0" {
				_ = json.NewEncoder(w).Encode([]map[string]any{})
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{entities["experiments/10"], entities["experiments/11"]})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/items":
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v2/experiments/") && strings.HasSuffix(r.URL.Path, "/uploads"):
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v2/experiments/"):
			if failNextGet {
				failNextGet = false
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"message":"get boom"}`))
				return
			}
			key := strings.TrimPrefix(r.URL.Path, "/api/v2/")
			if doc, ok := entities[key]; ok {
				_ = json.NewEncoder(w).Encode(doc)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v2/experiments/"):
			key := strings.TrimPrefix(r.URL.Path, "/api/v2/")
			var patch map[string]any
			_ = json.NewDecoder(r.Body).Decode(&patch)
			doc := entities[key]
			for k, v := range patch {
				doc[k] = v
			}
			_ = json.NewEncoder(w).Encode(doc)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	t.Setenv("ELABFTW_BASE_URL", srv.URL)
	t.Setenv("ELABFTW_API_KEY", "1-test-key")
	env := cli.Env{
		WorkDir:    t.TempDir(),
		ClientOpts: []client.Option{client.WithRootCAs(srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs)},
	}

	newDir := t.TempDir()
	newDoc := filepath.Join(newDir, "report.md")
	var out, errBuf bytes.Buffer
	code := cli.RunWithEnv(context.Background(), []string{"new", "Fresh Assay", "-o", newDoc}, nil, &out, &errBuf, env)
	if code != cli.ExitOK {
		t.Fatalf("new failed (code %d): stderr=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "created experiments/50:") {
		t.Fatalf("unexpected new stdout: %q", out.String())
	}
	raw, err := os.ReadFile(newDoc)
	if err != nil {
		t.Fatal(err)
	}
	meta, _, err := frontmatter.Parse(string(raw))
	if err != nil || meta.Values["id"] != 50 {
		t.Fatalf("expected id=50 in new doc, got %+v err=%v", meta, err)
	}

	failNextGet = true
	newDoc2 := filepath.Join(newDir, "second.md")
	out.Reset()
	errBuf.Reset()
	code = cli.RunWithEnv(context.Background(), []string{"new", "Flaky", "-o", newDoc2}, nil, &out, &errBuf, env)
	if code != cli.ExitError {
		t.Fatalf("expected error on GET failure, got %d", code)
	}
	saved51, err := state.Load(srv.URL, "experiments", "51")
	if err != nil || saved51 == nil || saved51["pending_create"] != true {
		t.Fatalf("expected resumable pending_create state for 51, got %v err=%v", saved51, err)
	}

	backupDir := t.TempDir()
	out.Reset()
	errBuf.Reset()
	code = cli.RunWithEnv(context.Background(), []string{"clone", "--all", backupDir}, nil, &out, &errBuf, env)
	if code != cli.ExitOK {
		t.Fatalf("clone --all failed (code %d): stdout=%q stderr=%q", code, out.String(), errBuf.String())
	}
	mdPath := filepath.Join(backupDir, "experiments", "10-Markdown Note", "report.md")
	htmlPath := filepath.Join(backupDir, "experiments", "11-HTML Note", "report.md")
	if _, err := os.Stat(mdPath); err != nil {
		t.Fatalf("cloned markdown doc missing: %v", err)
	}
	htmlBytes, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("html snapshot missing: %v", err)
	}
	if _, isDoc := frontmatter.TextMeta(string(htmlBytes)); isDoc {
		t.Fatalf("html snapshot must not contain elport front matter: %q", htmlBytes)
	}

	dupDoc := filepath.Join(t.TempDir(), "dup.md")
	out.Reset()
	errBuf.Reset()
	code = cli.RunWithEnv(context.Background(), []string{"clone", "10", "-o", dupDoc}, nil, &out, &errBuf, env)
	if code != cli.ExitError || !strings.Contains(errBuf.String(), "already has a working copy") {
		t.Fatalf("expected working copy refusal, code=%d stderr=%q", code, errBuf.String())
	}
	if _, err := os.Stat(dupDoc); !os.IsNotExist(err) {
		t.Fatalf("dup doc should not exist")
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelledDoc := filepath.Join(t.TempDir(), "cancelled.md")
	out.Reset()
	errBuf.Reset()
	code = cli.RunWithEnv(cancelCtx, []string{"clone", "11", "-o", cancelledDoc}, nil, &out, &errBuf, env)
	if code != cli.ExitError {
		t.Fatalf("expected error on cancelled context, got %d", code)
	}
	if _, err := os.Stat(cancelledDoc); !os.IsNotExist(err) {
		t.Fatalf("cancelled clone stub was not cleaned up")
	}
}

func strconvItoa(n int) string {
	return json.Number(string([]byte{byte('0' + n/10), byte('0' + n%10)})).String()
}

func TestCloneAllDoesNotDeleteConcurrentProcessDirectory(t *testing.T) {

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	backupDir := t.TempDir()
	otherDir := filepath.Join(backupDir, "experiments", "10-Race Note")
	otherDoc := filepath.Join(otherDir, "report.md")

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/experiments":
			if r.URL.Query().Get("offset") != "" && r.URL.Query().Get("offset") != "0" {
				_ = json.NewEncoder(w).Encode([]map[string]any{})
				return
			}

			_ = os.MkdirAll(otherDir, 0o755)
			_ = os.WriteFile(otherDoc, []byte("precious concurrent work"), 0o644)
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 10, "title": "Race Note"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/items":
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	t.Setenv("ELABFTW_BASE_URL", srv.URL)
	t.Setenv("ELABFTW_API_KEY", "1-test-key")
	env := cli.Env{
		WorkDir:    t.TempDir(),
		ClientOpts: []client.Option{client.WithRootCAs(srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs)},
	}

	var out, errBuf bytes.Buffer
	code := cli.RunWithEnv(context.Background(), []string{"clone", "--all", backupDir}, nil, &out, &errBuf, env)
	if code != cli.ExitError {
		t.Fatalf("expected failure for racing directory, got code=%d stdout=%q", code, out.String())
	}
	got, err := os.ReadFile(otherDoc)
	if err != nil || string(got) != "precious concurrent work" {
		t.Fatalf("concurrent process file was deleted or modified: content=%q err=%v", got, err)
	}
}
