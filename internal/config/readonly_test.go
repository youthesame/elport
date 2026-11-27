package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/youthesame/elport/internal/config"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadOnlyEnvValues(t *testing.T) {
	for _, tc := range []struct {
		val  string
		want bool
	}{
		{"", false}, {"0", false}, {"false", false}, {" FALSE ", false},
		{"1", true}, {"true", true}, {" True\n", true},
	} {
		home := setupHome(t)
		t.Setenv("ELPORT_READ_ONLY", tc.val)
		data, err := config.Load(home, home, nil)
		if err != nil {
			t.Fatalf("%q: %v", tc.val, err)
		}
		if data.ReadOnly != tc.want {
			t.Errorf("%q: ReadOnly = %v, want %v", tc.val, data.ReadOnly, tc.want)
		}
		wantSource := ""
		if tc.want {
			wantSource = "ELPORT_READ_ONLY"
		}
		if data.ReadOnlySource != wantSource {
			t.Errorf("%q: source = %q, want %q", tc.val, data.ReadOnlySource, wantSource)
		}
	}
	for _, bad := range []string{"yes", "no", "on", "2", "tru"} {
		home := setupHome(t)
		t.Setenv("ELPORT_READ_ONLY", bad)
		if _, err := config.Load(home, home, nil); err == nil || !strings.Contains(err.Error(), "ELPORT_READ_ONLY") {
			t.Errorf("%q: want explicit error, got %v", bad, err)
		}
	}
}

func TestReadOnlyLayersAreMonotonicAndValidated(t *testing.T) {
	home := setupHome(t)
	userCfg := filepath.Join(home, ".config", "elport", "config.toml")
	project := filepath.Join(home, "proj")
	docDir := filepath.Join(project, "doc")
	projCfg := filepath.Join(project, ".elport.toml")
	docCfg := filepath.Join(docDir, ".elport.toml")
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatal(err)
	}

	load := func() (*config.Data, error) { return config.Load(project, docDir, nil) }

	data, err := load()
	if err != nil || data.ReadOnly || data.ReadOnlySource != "" {
		t.Fatalf("no layers: %+v %v", data, err)
	}

	writeFile(t, userCfg, "read_only = true\n")
	writeFile(t, projCfg, "read_only = false\n")
	writeFile(t, docCfg, "read_only = false\n")
	data, err = load()
	if err != nil || !data.ReadOnly || data.ReadOnlySource != userCfg {
		t.Fatalf("user true must win: %+v %v", data, err)
	}
	if _, ok := data.Raw["read_only"]; ok {
		t.Fatal("read_only must not leak into Raw")
	}

	writeFile(t, userCfg, "read_only = false\n")
	writeFile(t, projCfg, "read_only = true\n")
	data, err = load()
	if err != nil || !data.ReadOnly || data.ReadOnlySource != projCfg {
		t.Fatalf("project true must win over doc false: %+v %v", data, err)
	}

	writeFile(t, projCfg, "")
	writeFile(t, docCfg, "read_only = true\n")
	data, err = load()
	if err != nil || !data.ReadOnly || data.ReadOnlySource != docCfg {
		t.Fatalf("doc dir true: %+v %v", data, err)
	}

	t.Setenv("ELPORT_READ_ONLY", "1")
	data, err = load()
	if err != nil || !data.ReadOnly || data.ReadOnlySource != "ELPORT_READ_ONLY" {
		t.Fatalf("env source must take precedence: %+v %v", data, err)
	}
	writeFile(t, docCfg, "read_only = \"yes\"\n")
	if _, err := load(); err == nil || !strings.Contains(err.Error(), docCfg) {
		t.Fatalf("invalid layer must fail even when env already enabled read-only: %v", err)
	}
	t.Setenv("ELPORT_READ_ONLY", "")

	for _, tc := range []struct {
		path string
		val  string
	}{
		{userCfg, `"yes"`}, {userCfg, "1"}, {projCfg, `"true"`}, {docCfg, "0"},
	} {
		writeFile(t, userCfg, "read_only = true\n")
		writeFile(t, projCfg, "")
		writeFile(t, docCfg, "")
		writeFile(t, tc.path, "read_only = "+tc.val+"\n")
		if _, err := load(); err == nil || !strings.Contains(err.Error(), "read_only must be true or false") || !strings.Contains(err.Error(), tc.path) {
			t.Errorf("%s = %s: want explicit error naming the file, got %v", tc.path, tc.val, err)
		}
	}
}
