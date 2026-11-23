package help_test

import (
	"strings"
	"testing"

	"github.com/youthesame/elport/internal/help"
)

func TestEmbeddedTopicsLoad(t *testing.T) {
	for _, name := range []string{"agents", "workflow", "documents"} {
		got, ok := help.Read(name)
		if !ok || strings.TrimSpace(got) == "" {
			t.Fatalf("missing or empty embedded topic %q", name)
		}
	}
}

func TestUnknownTopicReturnsFalse(t *testing.T) {
	if _, ok := help.Read("push"); ok {
		t.Fatal("expected unknown topic 'push' to return ok=false")
	}
}
