package help_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/youthesame/elport/internal/help"
)

func TestEmbeddedTopicsMatchPythonSource(t *testing.T) {
	// Keep Go embedded help topics byte-for-byte identical to the Python
	// copies while both implementations coexist in-tree before cutover.
	pyHelpDir := filepath.Join("..", "..", "src", "elport", "help")
	if _, err := os.Stat(pyHelpDir); os.IsNotExist(err) {
		t.Skip("python source removed after cutover")
	}
	for _, topic := range help.Topics {
		got, ok := help.Read(topic.Name)
		if !ok {
			t.Fatalf("missing embedded topic %q", topic.Name)
		}
		wantBytes, err := os.ReadFile(filepath.Join(pyHelpDir, topic.Name+".md"))
		if err != nil {
			t.Fatalf("read python help %q: %v", topic.Name, err)
		}
		if got != string(wantBytes) {
			t.Errorf("embedded topic %q drifted from src/elport/help/%s.md", topic.Name, topic.Name)
		}
	}
}

func TestUnknownTopicReturnsFalse(t *testing.T) {
	if _, ok := help.Read("push"); ok {
		t.Fatal("expected unknown topic 'push' to return ok=false")
	}
}
