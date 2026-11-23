package plan_test

import (
	"testing"

	"github.com/youthesame/elport/internal/plan"
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
