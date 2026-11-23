package cli_test

import (
	"bytes"
	"context"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/youthesame/elport/internal/cli"
	"github.com/youthesame/elport/internal/client"
	"github.com/youthesame/elport/internal/config"
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
	for _, want := range []string{"Ada Lovelace (uid 7)", "Profile   lab", "Team      Lab (#3) · admin", "API key   read-write", "Server    eLabFTW 6.0.9", "Scopes    experiments=team · items=self"} {
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
