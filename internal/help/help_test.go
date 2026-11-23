package help_test

import (
	"strings"
	"testing"

	"github.com/youthesame/elport/internal/help"
)

func TestEmbeddedTopicsLoadAndMatchSummary(t *testing.T) {
	summary := help.Summary()
	for _, topic := range help.Topics {
		got, ok := help.Read(topic.Name)
		if !ok || strings.TrimSpace(got) == "" {
			t.Fatalf("missing or empty embedded topic %q", topic.Name)
		}
		if !strings.Contains(summary, "elport help "+topic.Name) {
			t.Errorf("Summary() missing topic %q", topic.Name)
		}
	}
}

func TestUnknownTopicReturnsFalse(t *testing.T) {
	if _, ok := help.Read("push"); ok {
		t.Fatal("expected unknown topic 'push' to return ok=false")
	}
}
