package cli_test

import (
	"bytes"
	"context"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/youthesame/elport/internal/cli"
	"github.com/youthesame/elport/internal/client"
	"github.com/youthesame/elport/internal/config"
	"github.com/youthesame/elport/internal/state"
)

type memKeyring struct {
	items map[string]string
}

func (m *memKeyring) Get(service, user string) (string, error) {
	v, ok := m.items[service+":"+user]
	if !ok {
		return "", config.ErrNotFound
	}
	return v, nil
}

func (m *memKeyring) Set(service, user, password string) error {
	if m.items == nil {
		m.items = map[string]string{}
	}
	m.items[service+":"+user] = password
	return nil
}

func (m *memKeyring) Delete(service, user string) error {
	k := service + ":" + user
	if _, ok := m.items[k]; !ok {
		return config.ErrNotFound
	}
	delete(m.items, k)
	return nil
}

func TestRunLoginProfileWhoamiListActivityLogout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ELABFTW_BASE_URL", "")
	t.Setenv("ELABFTW_API_KEY", "")

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "12-secret-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v2/users/me":
			_, _ = w.Write([]byte(`{"userid":7,"fullname":"Ada Lovelace","email":"ada@example.test","team":3,"teams":[{"id":3,"name":"Lab","usergroup":2}],"is_sysadmin":false,"scope_experiments":2,"scope_items":1}`))
		case "/api/v2/info":
			_, _ = w.Write([]byte(`{"elabftw_version":"6.0.9"}`))
		case "/api/v2/apikeys":
			_, _ = w.Write([]byte(`[{"id":12,"can_write":1}]`))
		case "/api/v2/experiments":
			if r.URL.Query().Get("offset") != "0" {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_, _ = w.Write([]byte(`[{"id":42,"date":"2026-11-23","title":"Assay 1","created_at":"2026-11-23 10:00:00","userid":7,"fullname":"Ada Lovelace"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	noColor := false
	kr := &memKeyring{}
	env := cli.Env{
		WorkDir:    t.TempDir(),
		Keyring:    kr,
		ClientOpts: []client.Option{client.WithRootCAs(pool)},
		IsTTY:      &noColor,
		TermWidth:  30,
		Now:        func() time.Time { return time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC) },
	}

	var out, errBuf bytes.Buffer
	stdin := strings.NewReader(srv.URL + "\n12-secret-key\n")
	if code := cli.RunWithEnv(context.Background(), []string{"login", "lab"}, stdin, &out, &errBuf, env); code != cli.ExitOK {
		t.Fatalf("login failed: code=%d stderr=%q", code, errBuf.String())
	}

	out.Reset()
	errBuf.Reset()
	if code := cli.RunWithEnv(context.Background(), []string{"profile"}, nil, &out, &errBuf, env); code != cli.ExitOK {
		t.Fatalf("profile failed: code=%d stderr=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "* lab  "+srv.URL) {
		t.Errorf("unexpected profile output: %q", out.String())
	}

	out.Reset()
	errBuf.Reset()
	if code := cli.RunWithEnv(context.Background(), []string{"whoami"}, nil, &out, &errBuf, env); code != cli.ExitOK {
		t.Fatalf("whoami failed: code=%d stderr=%q", code, errBuf.String())
	}
	for _, want := range []string{" - Ada Lovelace (uid 7)", "Profile   lab", "Team      Lab (#3) · admin", "API key   read-write", "Server    eLabFTW 6.0.9", "Scopes    experiments=team · items=self"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("whoami output missing %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	errBuf.Reset()
	if code := cli.RunWithEnv(context.Background(), []string{"list"}, nil, &out, &errBuf, env); code != cli.ExitOK {
		t.Fatalf("list failed: code=%d stderr=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "42  2026-11-23  Assay 1") {
		t.Errorf("unexpected list output: %q", out.String())
	}

	out.Reset()
	errBuf.Reset()
	if code := cli.RunWithEnv(context.Background(), []string{"activity"}, nil, &out, &errBuf, env); code != cli.ExitOK {
		t.Fatalf("activity failed: code=%d stderr=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "Ada Lovelace  1 experiment · 1 active day") {
		t.Errorf("unexpected activity output:\n%s", out.String())
	}

	out.Reset()
	errBuf.Reset()
	if code := cli.RunWithEnv(context.Background(), []string{"logout", "lab"}, nil, &out, &errBuf, env); code != cli.ExitOK {
		t.Fatalf("logout failed: code=%d stderr=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "logged out profile lab") {
		t.Errorf("unexpected logout output: %q", out.String())
	}
}

func TestRunLoginRejectsEOFAndCanceledContext(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	kr := &memKeyring{}
	env := cli.Env{WorkDir: t.TempDir(), Keyring: kr}

	var out, errBuf bytes.Buffer
	if code := cli.RunWithEnv(context.Background(), []string{"login", "lab"}, strings.NewReader("https://elab.example.test\n"), &out, &errBuf, env); code != cli.ExitError {
		t.Fatalf("login with missing api_key line should exit 1, got %d", code)
	}
	if !strings.Contains(errBuf.String(), "api_key is required") {
		t.Errorf("unexpected stderr: %q", errBuf.String())
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	out.Reset()
	errBuf.Reset()
	if code := cli.RunWithEnv(canceled, []string{"login", "lab"}, strings.NewReader("https://elab.example.test\nsecret\n"), &out, &errBuf, env); code != cli.ExitError {
		t.Fatalf("login with canceled ctx should exit 1, got %d", code)
	}
}

func TestRunViewCommentsStatusDiffAndFetchEndToEnd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ELABFTW_BASE_URL", "")
	t.Setenv("ELABFTW_API_KEY", "")

	var postedComment string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/experiments/42":
			_, _ = w.Write([]byte(`{"id":42,"content_type":2,"body":"remote line\n","uploads":[{"id":9,"real_name":"a.csv","long_name":"x/a.csv","storage":1}],"steps":[{"ordering":1,"finished":1,"body":"Done step"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/experiments/42/uploads":
			_, _ = w.Write([]byte(`[{"id":9,"real_name":"a.csv","long_name":"x/a.csv","storage":1}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/experiments/42/uploads/9":
			_, _ = w.Write([]byte("col\n1\n"))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/experiments/42/comments":
			_, _ = w.Write([]byte(`[{"fullname":"Ada","created_at":"2026-11-23","modified_at":"2026-11-23","comment":"Hi"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/experiments/42/comments":
			postedComment = "ok"
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/info":
			_, _ = w.Write([]byte(`{"elabftw_version":"6.0.9"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	kr := &memKeyring{}
	work := t.TempDir()
	env := cli.Env{
		WorkDir:    work,
		Keyring:    kr,
		ClientOpts: []client.Option{client.WithRootCAs(pool)},
	}

	var out, errBuf bytes.Buffer
	if code := cli.RunWithEnv(context.Background(), []string{"login", "lab"}, strings.NewReader(srv.URL+"\n1-key\n"), &out, &errBuf, env); code != cli.ExitOK {
		t.Fatalf("login: %d %s", code, errBuf.String())
	}

	out.Reset()
	errBuf.Reset()
	entityURL := srv.URL + "/experiments.php?mode=view&id=42"
	if code := cli.RunWithEnv(context.Background(), []string{"view", entityURL}, nil, &out, &errBuf, env); code != cli.ExitOK {
		t.Fatalf("view URL: %d %s", code, errBuf.String())
	}
	if out.String() != "remote line\n" || !strings.Contains(errBuf.String(), "step [x] Done step") {
		t.Fatalf("view output mismatch: stdout=%q stderr=%q", out.String(), errBuf.String())
	}

	out.Reset()
	errBuf.Reset()
	if code := cli.RunWithEnv(context.Background(), []string{"comments", "42"}, nil, &out, &errBuf, env); code != cli.ExitOK {
		t.Fatalf("comments 42: %d %s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "Ada 2026-11-23\nHi") {
		t.Fatalf("comments output: %q", out.String())
	}

	out.Reset()
	errBuf.Reset()
	if code := cli.RunWithEnv(context.Background(), []string{"comment", "42", "hello"}, nil, &out, &errBuf, env); code != cli.ExitOK {
		t.Fatalf("comment 42: %d %s", code, errBuf.String())
	}
	if postedComment != "ok" {
		t.Fatal("comment was not posted")
	}

	docDir := filepath.Join(work, "exp42")
	_ = os.MkdirAll(docDir, 0o755)
	docPath := filepath.Join(docDir, "report.md")
	_ = os.WriteFile(docPath, []byte("---\nid: 42\nentity: experiments\nprofile: lab\n---\n\nlocal line\n"), 0o644)

	out.Reset()
	errBuf.Reset()
	if code := cli.RunWithEnv(context.Background(), []string{"status", docPath}, nil, &out, &errBuf, env); code != cli.ExitOK {
		t.Fatalf("status: %d %s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "mode: markdown") {
		t.Fatalf("status output: %q", out.String())
	}

	out.Reset()
	errBuf.Reset()
	if code := cli.RunWithEnv(context.Background(), []string{"diff", docPath}, nil, &out, &errBuf, env); code != cli.ExitOK {
		t.Fatalf("diff: %d %s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "-remote line") || !strings.Contains(out.String(), "+local line") {
		t.Fatalf("diff output: %q", out.String())
	}

	out.Reset()
	errBuf.Reset()
	if code := cli.RunWithEnv(context.Background(), []string{"fetch", docDir}, nil, &out, &errBuf, env); code != cli.ExitOK {
		t.Fatalf("batch fetch: %d %s", code, errBuf.String())
	}
	if got, err := os.ReadFile(filepath.Join(docDir, "a.csv")); err != nil || string(got) != "col\n1\n" {
		t.Fatalf("batch fetch did not save a.csv: (%q, %v)", string(got), err)
	}

	_ = cli.RunWithEnv(context.Background(), []string{"logout", "lab"}, nil, &out, &errBuf, env)
	_ = os.WriteFile(filepath.Join(home, ".config", "elport", "config.toml"), []byte("default_profile = \"lab\"\n[profiles.lab]\nbase_url = \""+srv.URL+"\"\n"), 0o600)
	_ = os.MkdirAll(filepath.Join(home, ".config", "elport", "state"), 0o700)
	out.Reset()
	errBuf.Reset()
	if code := cli.RunWithEnv(context.Background(), []string{"status", docPath}, nil, &out, &errBuf, env); code != cli.ExitError {
		t.Fatalf("status without credentials should fail at remote step, got %d", code)
	}
	if !strings.Contains(out.String(), "uploads local:") {
		t.Errorf("status should print local section before failing on missing credentials: stdout=%q stderr=%q", out.String(), errBuf.String())
	}
}

func TestRunBatchPushSharesMeCacheAcrossDocuments(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ELABFTW_BASE_URL", "")
	t.Setenv("ELABFTW_API_KEY", "")

	var meCalls, exp42Calls, exp43Calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/users/me":
			meCalls.Add(1)
			_, _ = w.Write([]byte(`{"userid":7,"team":3}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/experiments/42":
			exp42Calls.Add(1)
			_, _ = w.Write([]byte(`{"id":42,"title":"Doc 42","content_type":2,"body":"same 42\n","userid":7,"team":3,"uploads":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/experiments/43":
			exp43Calls.Add(1)
			_, _ = w.Write([]byte(`{"id":43,"title":"Doc 43","content_type":2,"body":"same 43\n","userid":7,"team":3,"uploads":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	kr := &memKeyring{}
	work := t.TempDir()
	env := cli.Env{
		WorkDir:    work,
		Keyring:    kr,
		ClientOpts: []client.Option{client.WithRootCAs(pool)},
	}

	var out, errBuf bytes.Buffer
	if code := cli.RunWithEnv(context.Background(), []string{"login", "lab"}, strings.NewReader(srv.URL+"\n1-key\n"), &out, &errBuf, env); code != cli.ExitOK {
		t.Fatalf("login failed: code=%d stderr=%q", code, errBuf.String())
	}

	for _, doc := range []struct {
		dir  string
		id   string
		body string
	}{
		{"a", "42", "same 42\n"},
		{"b", "43", "same 43\n"},
	} {
		sub := filepath.Join(work, doc.dir)
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sub, "report.md"), []byte("---\nid: "+doc.id+"\nentity: experiments\nprofile: lab\ntitle: Doc "+doc.id+"\n---\n\n"+doc.body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := state.Save(srv.URL, "experiments", doc.id, map[string]any{
			"remote_base": doc.body,
			"local_base":  doc.body,
			"meta_base":   map[string]any{"title": "Doc " + doc.id},
		}); err != nil {
			t.Fatal(err)
		}
	}

	out.Reset()
	errBuf.Reset()
	if code := cli.RunWithEnv(context.Background(), []string{"push", work}, nil, &out, &errBuf, env); code != cli.ExitOK {
		t.Fatalf("batch push failed: code=%d stdout=%q stderr=%q", code, out.String(), errBuf.String())
	}
	if exp42Calls.Load() != 1 || exp43Calls.Load() != 1 {
		t.Fatalf("expected both documents to be fetched once, got exp42=%d exp43=%d", exp42Calls.Load(), exp43Calls.Load())
	}
	if got := meCalls.Load(); got != 1 {
		t.Fatalf("batch push across 2 documents on same profile must call /users/me once, got %d", got)
	}
}
