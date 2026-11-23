package cli

import (
	"bytes"
	"testing"
)

func TestSanitizeTerminal(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain text, newline, and tab pass", "id\ttitle 日本語 ✓\n", "id\ttitle 日本語 ✓\n"},
		{"CRLF passes", "a\r\nb", "a\r\nb"},
		{"lone CR is shown", "real\rfake", "real^Mfake"},
		{"trailing CR is shown", "a\r", "a^M"},
		{"own SGR colors pass", "\x1b[32m✓\x1b[0m \x1b[38;5;240m·\x1b[0m", "\x1b[32m✓\x1b[0m \x1b[38;5;240m·\x1b[0m"},
		{"erase display is shown", "hi\x1b[2J\x1b[1;1Hfake", "hi^[[2J^[[1;1Hfake"},
		{"OSC title is shown", "Mallory\x1b]0;pwned\x07", "Mallory^[]0;pwned^G"},
		{"unterminated CSI is shown", "x\x1b[31", "x^[[31"},
		{"bare ESC is shown", "x\x1b", "x^["},
		{"NUL, backspace, and DEL are shown", "a\x00b\x08c\x7f", "a^@b^Hc^?"},
		{"C1 CSI is replaced", "a\u009b2Jb", "a�2Jb"},
		{"invalid UTF-8 passes", "a\xffb", "a\xffb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(sanitizeTerminal([]byte(tc.in))); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTermSafeLeavesNonTerminalWritersRaw(t *testing.T) {
	var buf bytes.Buffer
	w := termSafe(&buf)
	if _, err := w.Write([]byte("a\x1b[2Jb")); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "a\x1b[2Jb" {
		t.Fatalf("pipe output must stay raw, got %q", got)
	}
	if _, ok := fileOf(&buf); ok {
		t.Fatal("a buffer is not a file")
	}
}
