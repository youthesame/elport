package config_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/youthesame/elport/internal/config"
)

func TestAPIKeyRedactsInAllFmtVerbs(t *testing.T) {
	// Secrets must never appear in formatted logs regardless of the fmt verb.
	const raw = "super-secret-api-key-12345"
	key := config.NewAPIKey(raw)

	for _, verb := range []string{"%s", "%q", "%v", "%+v", "%#v"} {
		out := fmt.Sprintf(verb, key)
		if strings.Contains(out, raw) {
			t.Errorf("fmt.Sprintf(%q, key) leaked secret: %q", verb, out)
		}
		if !strings.Contains(out, "[REDACTED]") {
			t.Errorf("fmt.Sprintf(%q, key) = %q, expected [REDACTED]", verb, out)
		}
	}

	if got := key.Reveal(); got != raw {
		t.Errorf("Reveal() = %q, want %q", got, raw)
	}
	if key.Empty() {
		t.Error("non-empty key reported Empty() == true")
	}
	if !config.NewAPIKey("").Empty() {
		t.Error("empty key reported Empty() == false")
	}
}
