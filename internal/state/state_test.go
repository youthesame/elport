package state_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/youthesame/elport/internal/state"
)

func TestKeyMatchesPythonSHA256Format(t *testing.T) {
	// Key must match sha256("{base_url_without_trailing_slash}|{entity}|{id}")
	// with no filename suffix so Go reads Python-created state files directly.
	wantSum := sha256.Sum256([]byte("https://elab.example.test|experiments|42"))
	want := hex.EncodeToString(wantSum[:])

	if got := state.Key("https://elab.example.test", "experiments", "42"); got != want {
		t.Fatalf("Key = %q, want %q", got, want)
	}
	if got := state.Key("https://elab.example.test///", "experiments", "42"); got != want {
		t.Fatalf("Key with trailing slashes = %q, want %q", got, want)
	}
}

func TestLoadSavePreservesUnknownFieldsAndEmptyPendingRemote(t *testing.T) {
	// Unknown keys (for rollback or future protocol fields) and the distinction
	// between an absent "pending_remote" and "pending_remote": "" must survive round-trips.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	base := "https://elab.example.test"
	initial := map[string]any{
		"local_base":     "hello <world>\n",
		"remote_base":    "hello <world>\n",
		"pending_remote": "",
		"working_copy":   "/tmp/exp/report.md",
		"future_field":   map[string]any{"nested": true},
	}

	if err := state.Save(base, "experiments", "7", initial); err != nil {
		t.Fatalf("Save: %v", err)
	}

	p, err := state.Path(base, "experiments", "7")
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	wantPath := filepath.Join(home, ".config", "elport", "state", state.Key(base, "experiments", "7"))
	if p != wantPath {
		t.Fatalf("Path = %q, want %q", p, wantPath)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("Stat state file: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("state file perm = %o, want 0600", info.Mode().Perm())
		}
	}

	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var rawDecoded map[string]any
	if err := json.Unmarshal(raw, &rawDecoded); err != nil {
		t.Fatalf("Unmarshal raw: %v", err)
	}
	if _, hasPending := rawDecoded["pending_remote"]; !hasPending {
		t.Fatal("empty pending_remote key was dropped on Save")
	}

	loaded, err := state.Load(base, "experiments", "7")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(loaded, initial) {
		t.Errorf("Load mismatch:\ngot:  %#v\nwant: %#v", loaded, initial)
	}

	missing, err := state.Load(base, "experiments", "999")
	if err != nil || missing != nil {
		t.Errorf("missing state = (%v, %v), want (nil, nil)", missing, err)
	}

	if err := os.WriteFile(p, []byte("{corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	corrupt, err := state.Load(base, "experiments", "7")
	if err != nil || corrupt != nil {
		t.Errorf("corrupt state = (%v, %v), want (nil, nil)", corrupt, err)
	}
}
