package cli_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/youthesame/elport/internal/cli"
)

func TestRunHelpAndVersion(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := cli.Run([]string{"--version"}, nil, &out, &errBuf); code != cli.ExitOK {
		t.Fatalf("--version exit = %d, want 0 (stderr=%q)", code, errBuf.String())
	}
	if !strings.HasPrefix(out.String(), "elport ") || errBuf.Len() != 0 {
		t.Fatalf("unexpected --version output: stdout=%q stderr=%q", out.String(), errBuf.String())
	}

	out.Reset()
	errBuf.Reset()
	if code := cli.Run([]string{"help"}, nil, &out, &errBuf); code != cli.ExitOK {
		t.Fatalf("help exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "elport help agents") || errBuf.Len() != 0 {
		t.Fatalf("unexpected help output: stdout=%q stderr=%q", out.String(), errBuf.String())
	}

	out.Reset()
	errBuf.Reset()
	if code := cli.Run([]string{"help", "agents"}, nil, &out, &errBuf); code != cli.ExitOK {
		t.Fatalf("help agents exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "# Acting for a user") || errBuf.Len() != 0 {
		t.Fatalf("unexpected help agents output: stdout=%q stderr=%q", out.String(), errBuf.String())
	}

	out.Reset()
	errBuf.Reset()
	if code := cli.Run([]string{"push", "--help"}, nil, &out, &errBuf); code != cli.ExitOK {
		t.Fatalf("push --help exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "--dry-run") || errBuf.Len() != 0 {
		t.Fatalf("unexpected push --help output: stdout=%q stderr=%q", out.String(), errBuf.String())
	}
}

func TestParseUsageErrorsReturnExit2(t *testing.T) {
	cases := [][]string{
		{},
		{"unknown-cmd"},
		{"help", "push"},
		{"help", "agents", "extra"},
		{"push", "--bogus"},
		{"push", "--entity", "invalid"},
		{"list", "--all", "--limit", "1"},
		{"list", "--scope", "world"},
		{"list", "--order", "bogus"},
		{"list", "--sort", "up"},
		{"list", "unexpected-positional"},
		{"comment"},
		{"comment", "a", "b", "c"},
		{"new"},
		{"view"},
		{"profile", "delete"},
	}
	for _, args := range cases {
		var out, errBuf bytes.Buffer
		code := cli.Run(args, nil, &out, &errBuf)
		if code != cli.ExitUsage {
			t.Errorf("Run(%q) exit = %d, want %d (stdout=%q stderr=%q)", args, code, cli.ExitUsage, out.String(), errBuf.String())
		}
		if out.Len() != 0 || errBuf.Len() == 0 {
			t.Errorf("Run(%q) expected error on stderr only; stdout=%q stderr=%q", args, out.String(), errBuf.String())
		}
	}
}

func TestParseFlagAndPositionalCompatibility(t *testing.T) {
	t.Run("trailing flag after positional", func(t *testing.T) {
		var out bytes.Buffer
		cmd, code, err := cli.Parse([]string{"push", "doc.md", "--force"}, &out)
		if err != nil || code != cli.ExitOK {
			t.Fatalf("Parse failed: code=%d err=%v", code, err)
		}
		if !cmd.Force || !slices.Equal(cmd.Docs, []string{"doc.md"}) {
			t.Fatalf("got Force=%v Docs=%v", cmd.Force, cmd.Docs)
		}
	})

	t.Run("double dash protects leading-dash filename", func(t *testing.T) {
		var out bytes.Buffer
		cmd, code, err := cli.Parse([]string{"push", "--", "-file.md"}, &out)
		if err != nil || code != cli.ExitOK {
			t.Fatalf("Parse failed: code=%d err=%v", code, err)
		}
		if !slices.Equal(cmd.Docs, []string{"-file.md"}) {
			t.Fatalf("got Docs=%v, want [-file.md]", cmd.Docs)
		}
	})

	t.Run("interspersed flag between positionals intentionally allowed by pflag", func(t *testing.T) {
		// Unlike Python argparse (which exits 2 when a flag splits nargs="*"),
		// pflag permits standard GNU-style interspersed flags. Recorded here so
		// this deliberate syntax widening is explicit rather than accidental.
		var out bytes.Buffer
		cmd, code, err := cli.Parse([]string{"push", "a.md", "--force", "b.md"}, &out)
		if err != nil || code != cli.ExitOK {
			t.Fatalf("Parse failed: code=%d err=%v", code, err)
		}
		if !cmd.Force || !slices.Equal(cmd.Docs, []string{"a.md", "b.md"}) {
			t.Fatalf("got Force=%v Docs=%v", cmd.Force, cmd.Docs)
		}
	})

	t.Run("repeated --tag preserves commas via StringArray", func(t *testing.T) {
		var out bytes.Buffer
		cmd, code, err := cli.Parse([]string{"list", "--tag", "a,b", "--tag", "c"}, &out)
		if err != nil || code != cli.ExitOK {
			t.Fatalf("Parse failed: code=%d err=%v", code, err)
		}
		if !slices.Equal(cmd.Tags, []string{"a,b", "c"}) {
			t.Fatalf("got Tags=%v, want [a,b c]", cmd.Tags)
		}
	})

	t.Run("comment 1-arg and 2-arg shorthand", func(t *testing.T) {
		var out bytes.Buffer
		cmd, code, err := cli.Parse([]string{"comment", "hello"}, &out)
		if err != nil || code != cli.ExitOK {
			t.Fatalf("Parse failed: code=%d err=%v", code, err)
		}
		if cmd.Doc != "report.md" || cmd.Text != "hello" {
			t.Fatalf("got Doc=%q Text=%q", cmd.Doc, cmd.Text)
		}

		cmd, code, err = cli.Parse([]string{"comment", "357", "hello"}, &out)
		if err != nil || code != cli.ExitOK {
			t.Fatalf("Parse failed: code=%d err=%v", code, err)
		}
		if cmd.Doc != "357" || cmd.Text != "hello" {
			t.Fatalf("got Doc=%q Text=%q", cmd.Doc, cmd.Text)
		}
	})
}
