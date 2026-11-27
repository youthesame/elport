package cli

import (
	"os"
	"testing"

	"golang.org/x/term"

	"github.com/youthesame/elport/internal/config"
)

func TestClientCacheSeparatesReadOnlyClients(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ELABFTW_BASE_URL", "https://elab.example.test")
	t.Setenv("ELABFTW_API_KEY", "1-key")
	cache := newClientCache()
	raw := map[string]any{}
	_, ro, err := resolveClientCached(&config.Data{Raw: raw, ReadOnly: true}, "", "", nil, Env{}, cache)
	if err != nil {
		t.Fatal(err)
	}
	_, rw, err := resolveClientCached(&config.Data{Raw: raw}, "", "", nil, Env{}, cache)
	if err != nil {
		t.Fatal(err)
	}
	if ro == rw || !ro.ReadOnly() || rw.ReadOnly() {
		t.Fatalf("cache must not share clients across read-only modes: ro=%v rw=%v", ro.ReadOnly(), rw.ReadOnly())
	}
	_, again, _ := resolveClientCached(&config.Data{Raw: raw, ReadOnly: true}, "", "", nil, Env{}, cache)
	if again != ro {
		t.Fatal("same read-only key must reuse the cached client")
	}
}

func TestIsTerminalUnwrapsTermWriter(t *testing.T) {
	yes := true
	if !(Env{IsTTY: &yes}).isTerminal(nil) {
		t.Fatal("Env.IsTTY must override detection")
	}
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if (Env{}).isTerminal(termWriter{f: f}) {
		t.Fatal("a regular file is not a terminal")
	}
	if got, ok := fileOf(termWriter{f: f}); !ok || got != f {
		t.Fatal("fileOf must unwrap termWriter")
	}
	for _, dev := range []string{"/dev/tty", "/dev/ptmx"} {
		tty, err := os.OpenFile(dev, os.O_RDWR, 0)
		if err != nil {
			continue
		}
		defer func() { _ = tty.Close() }()
		if !term.IsTerminal(int(tty.Fd())) {
			continue
		}
		if !(Env{}).isTerminal(termWriter{f: tty}) {
			t.Fatal("isTerminal must see through the termSafe wrapper")
		}
		return
	}
	t.Skip("no terminal device available")
}
