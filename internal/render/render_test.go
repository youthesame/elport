package render_test

import (
	"strings"
	"testing"

	"github.com/youthesame/elport/internal/render"
)

func TestMarkdownToHTMLPreservesMathFiguresTablesAndStrikethrough(t *testing.T) {

	md := strings.Join([]string{
		"# Title",
		"",
		"Inline $a*b*c$, $\\{x\\}$, $a < b & c$, \\(u_1 + v_2\\), and price \\$100 to \\$200.",
		"",
		"$$",
		"\\begin{aligned}",
		"x &= a*b \\\\",
		"y &< c",
		"\\end{aligned}",
		"$$",
		"",
		"~~removed~~ and <sub>2</sub> and <sup>3</sup>.",
		"",
		"- [x] Done",
		"- [ ] Todo",
		"",
		"| Left | Right |",
		"| :--- | ---: |",
		"| 1 | 2 |",
		"",
		`<figure><img src="https://e.example/app/download.php?f=a%2Fb&name=fig.png&storage=1" width="80%"><figcaption>Fig 1</figcaption></figure>`,
		"",
		"`$a*b*$`",
		"",
	}, "\n")

	got, err := render.MarkdownToHTML(md)
	if err != nil {
		t.Fatalf("MarkdownToHTML: %v", err)
	}

	checks := []struct {
		desc string
		want string
	}{
		{"inline star math", "$a*b*c$"},
		{"inline braces math", `$\{x\}$`},
		{"inline lt/amp escaped", "$a &lt; b &amp; c$"},
		{"inline paren math", `\(u_1 + v_2\)`},
		{"escaped dollars", `\$100 to \$200`},
		{"display math", `x &amp;= a*b \\`},
		{"display math lt", `y &amp;&lt; c`},
		{"strikethrough s tag", "<s>removed</s>"},
		{"sub and sup", "<sub>2</sub> and <sup>3</sup>"},
		{"tasklist literal checked", "<li>[x] Done</li>"},
		{"tasklist literal unchecked", "<li>[ ] Todo</li>"},
		{"table align via style", `style="text-align:right"`},
		{"figure preserved", `<figure><img src="https://e.example/app/download.php?f=a%2Fb&name=fig.png&storage=1" width="80%"><figcaption>Fig 1</figcaption></figure>`},
		{"code span untouched", "<code>$a*b*$</code>"},
	}
	for _, c := range checks {
		if !strings.Contains(got, c.want) {
			t.Errorf("missing %s (%q) in:\n%s", c.desc, c.want, got)
		}
	}

	nested, err := render.MarkdownToHTML(`$\text{an inline $a*b*c$ expression} + x$` + "\n")
	if err != nil {
		t.Fatalf("MarkdownToHTML nested braces: %v", err)
	}
	if !strings.Contains(nested, `$\text{an inline $a*b*c$ expression} + x$`) || strings.Contains(nested, "<em>") {
		t.Errorf("nested TeX brace group mangled: %q", nested)
	}

	dollarImg, err := render.MarkdownToHTML("![photo](https://e.example/plot$2025$.png)\n")
	if err != nil {
		t.Fatalf("MarkdownToHTML dollar URL: %v", err)
	}
	if !strings.Contains(dollarImg, `src="https://e.example/plot$2025$.png"`) {
		t.Errorf("dollar URL mangled: %q", dollarImg)
	}
	if strings.Contains(got, "<del>") {
		t.Errorf("must not emit <del> (stripped by eLabFTW 6.x Filter::body):\n%s", got)
	}
	if strings.Contains(got, "<input") {
		t.Errorf("must not emit <input> checkbox (stripped by eLabFTW 6.x Filter::body):\n%s", got)
	}
	if strings.Contains(got, "align=") {
		t.Errorf("must not emit table align= attribute (stripped by eLabFTW 6.x Filter::body):\n%s", got)
	}
}

func TestCanonHTMLAbsorbsHarmlessNoiseAndPreservesSemanticChanges(t *testing.T) {

	a := "<!-- comment --><table>\n<thead>\n<tr><th style=\"text-align: right\">A</th><th colspan=\"2\">B</th></tr>\n</thead>\n<tbody>\n<tr><td rowspan=\"2\">1</td><td>2</td></tr>\n</tbody>\n</table><p>Hello\nworld</p><img width=\"80%\" src=\"app/download.php?f=a\" alt=\"old\">"
	b := `<table><tr><th style="text-align:right;">A</th><th colspan="2">B</th></tr><tr><td rowspan="2">1</td><td>2</td></tr></table><p>Hello world</p><img alt="download.php" src="app/download.php?f=a" width="80%">`

	ca := render.CanonHTML(a)
	cb := render.CanonHTML(b)
	if ca != cb {
		t.Fatalf("expected identical canonical HTML:\n--- A ---\n%s--- B ---\n%s", ca, cb)
	}

	pre1 := "<pre class=\"language-python\"><code>def f():\n    return 1\n</code></pre>"
	pre2 := "<pre class=\"language-python\"><code>def f():\n  return 1\n</code></pre>"
	if render.CanonHTML(pre1) == render.CanonHTML(pre2) {
		t.Errorf("CanonHTML must preserve whitespace inside <pre>/<code>")
	}

	if render.CanonHTML(`<img src="x" width="80%">`) == render.CanonHTML(`<img src="x" width="50%">`) {
		t.Error("width change must remain visible in CanonHTML")
	}
	if render.CanonHTML(`<a href="a">x</a>`) == render.CanonHTML(`<a href="b">x</a>`) {
		t.Error("href change must remain visible in CanonHTML")
	}
	if render.CanonHTML(`<table><tr><td colspan="2">x</td></tr></table>`) == render.CanonHTML(`<table><tr><td colspan="3">x</td></tr></table>`) {
		t.Error("colspan change must remain visible in CanonHTML")
	}
	if render.CanonHTML(`<p><b>a</b> <i>b</i></p>`) == render.CanonHTML(`<p><b>a</b><i>b</i></p>`) {
		t.Error("inline whitespace between elements must remain visible in CanonHTML")
	}
	if render.CanonHTML(`<p>foo<b> bar</b></p>`) == render.CanonHTML(`<p>foo<b>bar</b></p>`) {
		t.Error("leading inline space inside inline element must remain visible in CanonHTML")
	}
	if render.CanonHTML(`<img src="x" style="width:50%;width:80%">`) == render.CanonHTML(`<img src="x" style="width:80%;width:50%">`) {
		t.Error("CSS declaration order change must remain visible in CanonHTML")
	}
}

func FuzzMarkdownToHTMLAndCanon(f *testing.F) {
	f.Add("# h\n\n$a*b*$ \\$10 ~~x~~\n")
	f.Add("<figure><img src=\"a.png\" width=\"80%\"></figure>\n")
	f.Fuzz(func(t *testing.T, md string) {
		out, err := render.MarkdownToHTML(md)
		if err != nil {
			t.Fatalf("MarkdownToHTML: %v", err)
		}
		_ = render.CanonHTML(out)
	})
}
