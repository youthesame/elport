package config_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/youthesame/elport/internal/config"
)

func TestAPIKeyRedactsInAllFmtVerbsAndUnexportedFields(t *testing.T) {
	// Secrets must never appear in formatted logs, even when embedded in an
	// unexported struct field where fmt bypasses custom Format/String methods.
	const raw = "super-secret-api-key-12345"
	key := config.NewAPIKey(raw)

	type holder struct {
		key config.APIKey
	}
	h := holder{key: key}

	for _, verb := range []string{"%s", "%q", "%v", "%+v", "%#v"} {
		out := fmt.Sprintf(verb, key)
		if strings.Contains(out, raw) {
			t.Errorf("fmt.Sprintf(%q, key) leaked secret: %q", verb, out)
		}
		if !strings.Contains(out, "[REDACTED]") {
			t.Errorf("fmt.Sprintf(%q, key) = %q, expected [REDACTED]", verb, out)
		}

		wrapped := fmt.Sprintf(verb, h)
		if strings.Contains(wrapped, raw) {
			t.Errorf("fmt.Sprintf(%q, holder{key}) leaked secret: %q", verb, wrapped)
		}
	}

	rawJSON, err := json.Marshal(key)
	if err != nil {
		t.Fatalf("json.Marshal(key) error: %v", err)
	}
	if strings.Contains(string(rawJSON), raw) {
		t.Errorf("json.Marshal(key) leaked secret: %s", rawJSON)
	}

	if got := key.Reveal(); got != raw {
		t.Errorf("Reveal() = %q, want %q", got, raw)
	}
	if key.Empty() {
		t.Error("non-empty key reported Empty() == true")
	}
	var zero config.APIKey
	if !zero.Empty() || zero.Reveal() != "" {
		t.Errorf("zero key: Empty()=%v Reveal()=%q", zero.Empty(), zero.Reveal())
	}
	if !config.NewAPIKey("").Empty() {
		t.Error("empty key reported Empty() == false")
	}
}
