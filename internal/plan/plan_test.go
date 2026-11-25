package plan_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/youthesame/elport/internal/plan"
	"github.com/youthesame/elport/internal/transclude"
)

func TestZeroApprovedUploadPathIsInvalid(t *testing.T) {

	var p plan.ApprovedUploadPath
	if p.Valid() {
		t.Fatal("zero ApprovedUploadPath must not be valid")
	}
	if p.Path() != "" {
		t.Fatalf("expected empty path, got %q", p.Path())
	}
}

func TestApproveBlocksRemoteLinkedUnlessListed(t *testing.T) {

	dir := t.TempDir()
	localFile := filepath.Join(dir, "mine.png")
	remoteLinked := filepath.Join(dir, "planted.csv")
	listedRemote := filepath.Join(dir, "approved.csv")
	for _, p := range []string{localFile, remoteLinked, listedRemote} {
		if err := os.WriteFile(p, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	body := "![a](mine.png) [b](./planted.csv) [c](approved.csv)\n"
	remoteBodies := []string{"[planted](planted.csv) [ok](approved.csv)\n"}
	var warn bytes.Buffer

	res, err := plan.Approve(body, dir, nil, []string{"approved.csv"}, remoteBodies, nil, &warn)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if !strings.Contains(warn.String(), "linked from the remote body") || !strings.Contains(warn.String(), "./planted.csv") {
		t.Errorf("expected remote-linked warning, got %q", warn.String())
	}

	resolvedMine := transclude.ResolveNonStrict(localFile)
	resolvedPlanted := transclude.ResolveNonStrict(remoteLinked)
	resolvedApproved := transclude.ResolveNonStrict(listedRemote)

	if len(res.Files) != 2 {
		t.Fatalf("expected 2 approved files, got %d (%+v)", len(res.Files), res.Files)
	}
	if !res.ByPath[resolvedMine].Valid() {
		t.Error("expected mine.png to be approved")
	}
	if !res.ByPath[resolvedApproved].Valid() {
		t.Error("expected listed approved.csv to be approved")
	}
	if res.ByPath[resolvedPlanted].Valid() {
		t.Error("remote-linked planted.csv must not be approved")
	}
	if res.Refs[1].File != "" {
		t.Errorf("expected remote-linked ref.File to be cleared, got %q", res.Refs[1].File)
	}
}

func TestApproveFromLocalRejectsUnvalidatedAndCaseInsensitiveRemoteLinked(t *testing.T) {

	var forged plan.LocalResult
	if _, err := plan.ApproveFromLocal(&forged, t.TempDir(), nil, nil, nil, nil); err == nil {
		t.Fatal("expected unvalidated LocalResult to be rejected")
	}

	dir := t.TempDir()
	dataFile := filepath.Join(dir, "DATA.csv")
	if err := os.WriteFile(dataFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	infoA, errA := os.Stat(filepath.Join(dir, "DATA.csv"))
	infoB, errB := os.Stat(filepath.Join(dir, "data.csv"))
	if errA != nil || errB != nil || !os.SameFile(infoA, infoB) {
		t.Skip("filesystem is case-sensitive")
	}

	var warn bytes.Buffer
	res, err := plan.Approve("[d](./data.csv#top)\n", dir, nil, nil, []string{"[r](DATA.csv)\n"}, nil, &warn)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if len(res.Files) != 0 {
		t.Fatalf("expected case-variant remote-linked path to be excluded, got %+v", res.Files)
	}
}

func TestApprovedUploadPathRejectsPostApprovalSwap(t *testing.T) {

	dir := t.TempDir()
	target := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := plan.Approve("[d](data.csv)\n", dir, nil, nil, nil, nil, nil)
	if err != nil || len(res.Files) != 1 {
		t.Fatalf("Approve failed: res=%+v err=%v", res, err)
	}
	approved := res.Files[0]

	other := filepath.Join(dir, "other.csv")
	if err := os.WriteFile(other, []byte("swapped"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(other, target); err != nil {
		t.Fatal(err)
	}
	if f, err := approved.Open(); err == nil {
		_ = f.Close()
		t.Fatal("expected Open to reject replaced file")
	}

	if runtime.GOOS != "windows" {
		secret := filepath.Join(dir, ".secret")
		if err := os.WriteFile(secret, []byte("top-secret"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(secret, target); err != nil {
			t.Fatal(err)
		}
		if f, err := approved.Open(); err == nil {
			_ = f.Close()
			t.Fatal("expected Open to reject post-approval symlink swap")
		}
	}
}

func TestApproveFromLocalSourcesBlocksHTMLBacktickRemoteLinkedPath(t *testing.T) {

	dir := t.TempDir()
	for _, name := range []string{"mine.png", "planted.csv", "t"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	local, err := plan.LocalCandidatesHTML("Math $x_{[1]}(t)$ ![a](mine.png) [b](planted.csv)\n", dir, nil, nil, nil)
	if err != nil {
		t.Fatalf("LocalCandidatesHTML: %v", err)
	}
	var warn bytes.Buffer
	res, err := plan.ApproveFromLocalSources(
		local,
		dir,
		nil,
		[]plan.RemoteSource{{Body: "<p>`<img src=\"planted.csv\">`</p>", ContentType: 1}},
		nil,
		&warn,
	)
	if err != nil {
		t.Fatalf("ApproveFromLocalSources: %v", err)
	}
	if len(res.Files) != 1 || filepath.Base(res.Files[0].Path()) != "mine.png" {
		t.Fatalf("approved files = %+v, want only mine.png", res.Files)
	}
	if !strings.Contains(warn.String(), "planted.csv") {
		t.Errorf("expected remote-linked warning for planted.csv, got %q", warn.String())
	}
}
