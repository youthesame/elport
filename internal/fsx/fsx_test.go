package fsx_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/youthesame/elport/internal/fsx"
)

func TestConfigDirAlwaysUsesDotConfigElport(t *testing.T) {
	// All OSes (macOS, Linux, Windows) must share <home>/.config/elport so Go
	// reads Python-created config and state without platform-specific paths.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	got, err := fsx.ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir() error: %v", err)
	}
	want := filepath.Join(home, ".config", "elport")
	if got != want {
		t.Fatalf("ConfigDir() = %q, want %q", got, want)
	}
}

func TestAtomicWriteFilePreservesPermissions(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "doc.md")
	if err := fsx.AtomicWriteFile(target, []byte("first"), 0o640); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := fsx.AtomicWriteFile(target, []byte("second"), 0o600); err != nil {
		t.Fatalf("second write: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "second" {
		t.Fatalf("got %q, want %q", string(got), "second")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(target)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if info.Mode().Perm() != 0o640 {
			t.Fatalf("perm = %o, want 0640", info.Mode().Perm())
		}
	}
}

func TestRootDirRefusesSymlinkAndEscape(t *testing.T) {
	// os.Root blocks escaping the root, while Lstat also refuses overwriting an
	// internal symlink pointing inside the document directory.
	dir := t.TempDir()
	root, err := fsx.OpenRoot(dir)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	defer func() { _ = root.Close() }()

	if err := root.AtomicWrite("ok.md", []byte("hello"), 0o644); err != nil {
		t.Fatalf("AtomicWrite ok.md: %v", err)
	}
	if err := root.AtomicWrite("../escape.md", []byte("bad"), 0o644); err == nil {
		t.Fatal("expected error when writing outside root")
	}

	if runtime.GOOS != "windows" {
		if err := os.Symlink("ok.md", filepath.Join(dir, "link.md")); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		err := root.AtomicWrite("link.md", []byte("overwrite"), 0o644)
		if err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("expected symlink refusal, got %v", err)
		}
	}
}
