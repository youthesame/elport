package plan_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/youthesame/elport/internal/plan"
	"github.com/youthesame/elport/internal/transclude"
)

func TestZeroApprovedUploadPathIsInvalid(t *testing.T) {
	// Zero value must never be treated as an approved upload target.
	var p plan.ApprovedUploadPath
	if p.Valid() {
		t.Fatal("zero ApprovedUploadPath must not be valid")
	}
	if p.Path() != "" {
		t.Fatalf("expected empty path, got %q", p.Path())
	}
}

func TestApproveBlocksRemoteLinkedUnlessListed(t *testing.T) {
	// A path appearing in a remote body or remembered remote_paths must never
	// receive an ApprovedUploadPath unless explicitly listed under attachments:.
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
