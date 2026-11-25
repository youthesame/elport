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
	if code := cli.Run([]string{"--version", "--help"}, nil, &out, &errBuf); code != cli.ExitOK {
		t.Fatalf("--version --help exit = %d, want 0", code)
	}
	if !strings.HasPrefix(out.String(), "elport ") || errBuf.Len() != 0 {
		t.Fatalf("--version --help should print version: stdout=%q stderr=%q", out.String(), errBuf.String())
	}

	out.Reset()
	errBuf.Reset()
	if code := cli.Run([]string{"--help"}, nil, &out, &errBuf); code != cli.ExitOK {
		t.Fatalf("--help exit = %d, want 0", code)
	}
	for _, want := range []string{"side effects:", "exit codes:", "ELABFTW_API_KEY", "elport help agents"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("root --help missing %q", want)
		}
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

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"push", "--help"}, "--dry-run"},
		{[]string{"comment", "--help"}, "[doc] <text>"},
		{[]string{"clone", "--help"}, "[ID_OR_URL | --all ROOT]"},
	} {
		out.Reset()
		errBuf.Reset()
		if code := cli.Run(tc.args, nil, &out, &errBuf); code != cli.ExitOK {
			t.Fatalf("%v exit = %d, want 0", tc.args, code)
		}
		if !strings.Contains(out.String(), tc.want) || errBuf.Len() != 0 {
			t.Fatalf("%v missing %q: stdout=%q stderr=%q", tc.args, tc.want, out.String(), errBuf.String())
		}
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
		{"push", "--entity="},
		{"push", "--entity", "bad", "--entity", "items"},
		{"list", "--all", "--limit", "1"},
		{"list", "--scope="},
		{"list", "--scope", "world"},
		{"list", "--order", "bogus"},
		{"list", "--sort", "up"},
		{"list", "--limit", "0x10"},
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

func TestRunRuntimeValidationErrorsReturnExit1(t *testing.T) {
	cases := []struct {
		args      []string
		wantError string
	}{
		{[]string{"clone"}, "exactly one of <id> or --all"},
		{[]string{"clone", "42", "--all", "backup/"}, "exactly one of <id> or --all"},
		{[]string{"profile", "use"}, "profile use requires a profile name"},
	}
	for _, tc := range cases {
		var out, errBuf bytes.Buffer
		code := cli.Run(tc.args, nil, &out, &errBuf)
		if code != cli.ExitError {
			t.Errorf("Run(%q) exit = %d, want %d", tc.args, code, cli.ExitError)
		}
		if out.Len() != 0 || !strings.Contains(errBuf.String(), tc.wantError) {
			t.Errorf("Run(%q) stderr = %q, want substring %q", tc.args, errBuf.String(), tc.wantError)
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

	t.Run("list decimal integers match Python int() including leading zeros and negative runtime check", func(t *testing.T) {

		var out bytes.Buffer
		cmd, code, err := cli.Parse([]string{"list", "--limit", "010", "--offset", "08"}, &out)
		if err != nil || code != cli.ExitOK {
			t.Fatalf("Parse failed: code=%d err=%v", code, err)
		}
		if cmd.Limit != 10 || cmd.Offset != 8 {
			t.Fatalf("got Limit=%d Offset=%d, want 10 and 8", cmd.Limit, cmd.Offset)
		}

		cmd, code, err = cli.Parse([]string{"list", "--limit", "0", "--offset", "-1"}, &out)
		if err != nil || code != cli.ExitOK {
			t.Fatalf("Parse negative/zero should not be usage error: code=%d err=%v", code, err)
		}
		if cmd.Limit != 0 || cmd.Offset != -1 {
			t.Fatalf("got Limit=%d Offset=%d", cmd.Limit, cmd.Offset)
		}
	})

	t.Run("clone --all empty value sets HasAll true", func(t *testing.T) {
		var out bytes.Buffer
		cmd, code, err := cli.Parse([]string{"clone", "--all="}, &out)
		if err != nil || code != cli.ExitOK {
			t.Fatalf("Parse failed: code=%d err=%v", code, err)
		}
		if !cmd.HasAll || cmd.AllRoot != "" {
			t.Fatalf("got HasAll=%v AllRoot=%q", cmd.HasAll, cmd.AllRoot)
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
