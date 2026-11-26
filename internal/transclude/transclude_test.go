package transclude_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/youthesame/elport/internal/transclude"
)

type trFixture struct {
	Extract []struct {
		Name  string `json:"name"`
		Input string `json:"input"`
		Refs  []struct {
			Path      string `json:"path"`
			Kind      string `json:"kind"`
			ByteStart int    `json:"byte_start"`
			ByteEnd   int    `json:"byte_end"`
			Slice     string `json:"slice"`
		} `json:"refs"`
	} `json:"extract"`
	HasRawHTML []struct {
		Name       string `json:"name"`
		Input      string `json:"input"`
		HasRawHTML bool   `json:"has_raw_html"`
	} `json:"has_raw_html"`
	BarePaths []struct {
		Name  string   `json:"name"`
		Input string   `json:"input"`
		Paths []string `json:"paths"`
	} `json:"bare_paths"`
	Ignore []struct {
		Name     string   `json:"name"`
		Relative string   `json:"relative"`
		Patterns []string `json:"patterns"`
		Prefix   string   `json:"prefix"`
		Ignored  bool     `json:"ignored"`
	} `json:"ignore"`
	NameUploads struct {
		Input  []transclude.Upload `json:"input"`
		Output []transclude.Upload `json:"output"`
	} `json:"name_uploads"`
	DownloadURL []struct {
		Upload transclude.Upload `json:"upload"`
		URL    string            `json:"url"`
		Parsed []string          `json:"parsed"`
	} `json:"download_url"`
	Reverse struct {
		BaseURL   string              `json:"base_url"`
		Uploads   []transclude.Upload `json:"uploads"`
		Input     string              `json:"input"`
		Output    string              `json:"output"`
		PlacedIDs []int               `json:"placed_ids"`
		Unmatched []string            `json:"unmatched"`
	} `json:"reverse"`
	Relink struct {
		Input    string                       `json:"input"`
		DocLinks map[string]map[string]string `json:"doc_links"`
		Output   string                       `json:"output"`
	} `json:"relink"`
	AbsoluteLinks struct {
		BaseURL string `json:"base_url"`
		Input   string `json:"input"`
		Output  string `json:"output"`
	} `json:"absolute_links"`
	SafeName struct {
		POSIX   map[string]string `json:"posix"`
		Windows map[string]string `json:"windows"`
	} `json:"safe_name"`
	ReadableHTML []struct {
		Name   string `json:"name"`
		Input  string `json:"input"`
		Output string `json:"output"`
	} `json:"readable_html"`
}

func loadFixture(t *testing.T) trFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", "transclude.json"))
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	var fx trFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatalf("unmarshal golden fixture: %v", err)
	}
	return fx
}

func TestTranscludeGoldenExtract(t *testing.T) {
	fx := loadFixture(t)
	for _, tc := range fx.Extract {
		t.Run(tc.Name, func(t *testing.T) {
			got := transclude.Extract(tc.Input)
			if len(got) != len(tc.Refs) {
				t.Fatalf("len(refs) = %d (%+v), want %d (%+v)", len(got), got, len(tc.Refs), tc.Refs)
			}
			for i, want := range tc.Refs {
				r := got[i]
				if r.Path != want.Path || r.Kind != want.Kind || r.Start != want.ByteStart || r.End != want.ByteEnd {
					t.Errorf("ref[%d] = {Path:%q Kind:%q Start:%d End:%d}, want {Path:%q Kind:%q Start:%d End:%d}",
						i, r.Path, r.Kind, r.Start, r.End, want.Path, want.Kind, want.ByteStart, want.ByteEnd)
				}
				if tc.Input[r.Start:r.End] != want.Slice {
					t.Errorf("ref[%d] slice = %q, want %q", i, tc.Input[r.Start:r.End], want.Slice)
				}
			}
		})
	}
}

func TestTranscludeGoldenHasRawHTML(t *testing.T) {
	fx := loadFixture(t)
	for _, tc := range fx.HasRawHTML {
		t.Run(tc.Name, func(t *testing.T) {
			if got := transclude.HasRawHTML(tc.Input); got != tc.HasRawHTML {
				t.Errorf("HasRawHTML(%q) = %v, want %v", tc.Input, got, tc.HasRawHTML)
			}
		})
	}
}

func TestTranscludeGoldenBarePaths(t *testing.T) {
	fx := loadFixture(t)
	for _, tc := range fx.BarePaths {
		t.Run(tc.Name, func(t *testing.T) {
			got := transclude.BarePaths(tc.Input)
			if !slices.Equal(got, tc.Paths) {
				t.Errorf("BarePaths = %v, want %v", got, tc.Paths)
			}
		})
	}
}

func TestTranscludeGoldenIgnore(t *testing.T) {
	fx := loadFixture(t)
	for _, tc := range fx.Ignore {
		t.Run(tc.Name, func(t *testing.T) {
			layer := transclude.IgnoreLayer{Patterns: tc.Patterns, Prefix: tc.Prefix}
			if got := transclude.Ignored(tc.Relative, []transclude.IgnoreLayer{layer}); got != tc.Ignored {
				t.Errorf("Ignored(%q, %+v) = %v, want %v", tc.Relative, layer, got, tc.Ignored)
			}
		})
	}
}

func TestTranscludeGoldenUploadsAndURLs(t *testing.T) {
	fx := loadFixture(t)

	gotNamed := transclude.NameUploads(fx.NameUploads.Input)
	if !reflect.DeepEqual(gotNamed, fx.NameUploads.Output) {
		t.Errorf("NameUploads = %+v, want %+v", gotNamed, fx.NameUploads.Output)
	}

	for _, tc := range fx.DownloadURL {
		gotURL := transclude.DownloadURL(fx.Reverse.BaseURL, tc.Upload)
		if gotURL != tc.URL {
			t.Errorf("DownloadURL(%+v) = %q, want %q", tc.Upload, gotURL, tc.URL)
		}
		f, name, storage, ok := transclude.ParseDownloadURL(gotURL)
		if !ok || !slices.Equal([]string{f, name, storage}, tc.Parsed) {
			t.Errorf("ParseDownloadURL(%q) = (%q,%q,%q,%v), want %v", gotURL, f, name, storage, ok, tc.Parsed)
		}
	}

	revOut, placed := transclude.Reverse(fx.Reverse.Input, fx.Reverse.Uploads, fx.Reverse.BaseURL)
	if revOut != fx.Reverse.Output {
		t.Errorf("Reverse output:\ngot:\n%q\nwant:\n%q", revOut, fx.Reverse.Output)
	}
	var gotIDs []int
	for _, u := range placed {
		gotIDs = append(gotIDs, u.ID)
	}
	if !slices.Equal(gotIDs, fx.Reverse.PlacedIDs) {
		t.Errorf("Reverse placed IDs = %v, want %v", gotIDs, fx.Reverse.PlacedIDs)
	}
	if gotUnmatched := transclude.UnmatchedDownloadURLs(fx.Reverse.Input, fx.Reverse.Uploads, fx.Reverse.BaseURL); !slices.Equal(gotUnmatched, fx.Reverse.Unmatched) {
		t.Errorf("UnmatchedDownloadURLs = %v, want %v", gotUnmatched, fx.Reverse.Unmatched)
	}

	if got := transclude.Relink(fx.Relink.Input, fx.Relink.DocLinks); got != fx.Relink.Output {
		t.Errorf("Relink = %q, want %q", got, fx.Relink.Output)
	}
	if got := transclude.AbsoluteLinks(fx.AbsoluteLinks.Input, fx.AbsoluteLinks.BaseURL); got != fx.AbsoluteLinks.Output {
		t.Errorf("AbsoluteLinks = %q, want %q", got, fx.AbsoluteLinks.Output)
	}
}

func TestTranscludeGoldenSafeName(t *testing.T) {
	fx := loadFixture(t)
	for in, want := range fx.SafeName.POSIX {
		if got := transclude.SafeNameForPlatform(in, false); got != want {
			t.Errorf("SafeName POSIX (%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range fx.SafeName.Windows {
		if got := transclude.SafeNameForPlatform(in, true); got != want {
			t.Errorf("SafeName Windows (%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTranscludeGoldenReadableHTML(t *testing.T) {
	fx := loadFixture(t)
	for _, tc := range fx.ReadableHTML {
		t.Run(tc.Name, func(t *testing.T) {
			if got := transclude.ReadableHTML(tc.Input); got != tc.Output {
				t.Errorf("ReadableHTML(%q) = %q, want %q", tc.Input, got, tc.Output)
			}
		})
	}
}

func TestPlanDotfileSymlinkAndDocumentLinks(t *testing.T) {

	root := t.TempDir()
	docDir := filepath.Join(root, ".hidden-workspace", "exp1")
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docDir, "fig.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docDir, ".secret"), []byte("no"), 0o644); err != nil {
		t.Fatal(err)
	}
	otherDoc := filepath.Join(docDir, "other.md")
	if err := os.WriteFile(otherDoc, []byte("---\nid: 7\nprofile: lab\n---\nBody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		secretDir := filepath.Join(root, ".ssh")
		if err := os.MkdirAll(secretDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(secretDir, "id_rsa"), []byte("priv"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(secretDir, "id_rsa"), filepath.Join(docDir, "key.txt")); err != nil {
			t.Fatal(err)
		}
	}

	var warn bytes.Buffer
	md := "![ok](fig.png) [secret](.secret) [sym](key.txt) [doc](other.md#sec)\n"
	refs, err := transclude.Plan(md, docDir, nil, &warn)
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}
	if len(refs) != 4 {
		t.Fatalf("expected 4 refs, got %d", len(refs))
	}
	if refs[0].File == "" {
		t.Error("expected fig.png to be planned even when docDir has hidden ancestor")
	}
	if refs[1].File != "" {
		t.Errorf("hidden .secret must not be planned: %+v", refs[1])
	}
	if runtime.GOOS != "windows" && refs[2].File != "" {
		t.Errorf("symlink into .ssh must not be planned: %+v", refs[2])
	}
	if refs[3].Doc == "" || refs[3].Fragment != "#sec" {
		t.Errorf("expected document link with fragment #sec, got %+v", refs[3])
	}
}

func TestNestedBracketsNoDoubleExtractAndReplaceNoPanic(t *testing.T) {

	dir := t.TempDir()
	dataFile := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(dataFile, []byte("a,b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	md := "[see [data](data.csv)]\n"
	refs, err := transclude.Plan(md, dir, nil, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("len(refs) = %d (%+v), want 1", len(refs), refs)
	}
	out := transclude.ReplaceSpans(md, refs, map[string]string{dataFile: "https://elab.example.test/x"})
	if out != "[see [data](https://elab.example.test/x)]\n" {
		t.Fatalf("ReplaceSpans = %q", out)
	}
}

func TestAbsoluteHiddenSegmentAndSymlinkDotDotResolution(t *testing.T) {

	root := t.TempDir()
	work := filepath.Join(root, "work")
	otherSub := filepath.Join(root, "other", "sub")
	_ = os.MkdirAll(work, 0o755)
	_ = os.MkdirAll(otherSub, 0o755)
	_ = os.WriteFile(filepath.Join(work, "data.csv"), []byte("wrong"), 0o644)
	otherData := filepath.Join(root, "other", "data.csv")
	_ = os.WriteFile(otherData, []byte("right"), 0o644)

	absHidden := work + "/.private/../data.csv"
	refs, err := transclude.Plan("[h]("+absHidden+")\n", work, nil, nil)
	if err != nil {
		t.Fatalf("Plan hidden abs: %v", err)
	}
	if len(refs) != 1 || refs[0].File != "" {
		t.Fatalf("expected absolute /.private/../ path to be rejected as hidden, got %+v", refs)
	}

	if runtime.GOOS != "windows" {
		if err := os.Symlink(otherSub, filepath.Join(work, "link")); err != nil {
			t.Fatal(err)
		}
		refs, err = transclude.Plan("[s](link/../data.csv)\n", work, nil, nil)
		if err != nil {
			t.Fatalf("Plan symlink..: %v", err)
		}
		want := transclude.ResolveNonStrict(otherData)
		if len(refs) != 1 || refs[0].File != want {
			t.Fatalf("link/../data.csv resolved to %q, want %q", refs[0].File, want)
		}
	}
}

func TestQueryEmptyFirstParamNameUploadsOrderSafeNameWinDriveAndReadableHTML(t *testing.T) {
	if f, name, storage, ok := transclude.ParseDownloadURL("https://elab.example.test/app/download.php?f=&f=hash&name=&name=a.png&storage=&storage=1"); !ok || f != "hash" || name != "a.png" || storage != "1" {
		t.Fatalf("ParseDownloadURL empty-first = (%q,%q,%q,%v)", f, name, storage, ok)
	}

	uploads := []transclude.Upload{
		{ID: 20, RealName: "image.png", LongName: "b"},
		{ID: 10, RealName: "image.png", LongName: "a"},
	}
	named := transclude.NameUploads(uploads)
	if named[0].ID != 20 || named[0].ElportName != "image-20.png" || named[1].ID != 10 || named[1].ElportName != "image.png" {
		t.Fatalf("NameUploads did not preserve input order: %+v", named)
	}

	if got := transclude.SafeNameForPlatform("C:report.csv", true); got != "report.csv" {
		t.Errorf("SafeNameForPlatform(C:report.csv, win) = %q, want report.csv", got)
	}
	if got := transclude.SafeNameForPlatform("C:report.csv", false); got != "C_report.csv" {
		t.Errorf("SafeNameForPlatform(C:report.csv, posix) = %q, want C_report.csv", got)
	}

	if got := transclude.ReadableHTML("<p>x</p>\n\n&amp;\n\n<p>y</p>"); got != "<p>x</p>\n&amp;\n<p>y</p>" {
		t.Errorf("ReadableHTML around entity = %q, want %q", got, "<p>x</p>\n&amp;\n<p>y</p>")
	}
	if got := transclude.ReadableHTML("<pre/>\n\nx\n\n"); got != "<pre>\n\nx\n\n" {
		t.Errorf("ReadableHTML after <pre/> = %q, want %q", got, "<pre>\n\nx\n\n")
	}
}

func TestExtractNeverMatchesInsideCodeFenceRapid(t *testing.T) {

	rapid.Check(t, func(t *rapid.T) {
		inner := rapid.StringMatching(`[A-Za-z0-9_./ -]{1,20}`).Draw(t, "path")
		md := "```\n![x](" + inner + ")\n<img src=\"" + inner + "\">\n```\n"
		if refs := transclude.Extract(md); len(refs) != 0 {
			t.Fatalf("extracted %+v inside code fence for %q", refs, md)
		}
	})
}

func TestSafeNameNeverContainsPathSepOrControlCharsRapid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		raw := rapid.String().Draw(t, "raw")
		for _, isWin := range []bool{false, true} {
			got := transclude.SafeNameForPlatform(raw, isWin)
			if got == "" || got == "." || got == ".." {
				t.Fatalf("unsafe empty/dot result %q for %q", got, raw)
			}
			if strings.ContainsAny(got, "/\\<>:?`") {
				t.Fatalf("unsafe character in %q for %q", got, raw)
			}
			for _, r := range got {
				if r >= 0 && r <= 0x1f {
					t.Fatalf("control char %U in %q for %q", r, got, raw)
				}
			}
		}
	})
}

func FuzzExtract(f *testing.F) {
	f.Add("![a](fig.png)\n")
	f.Add("```\n![skip](a.png)\n```\n<img src=\"b.png\">\n")
	f.Add("日本語\u3000🧪 [x](<a (1).png> \"t\")\x1c\n")
	f.Fuzz(func(t *testing.T, text string) {
		refs := transclude.Extract(text)
		for _, r := range refs {
			if r.Start < 0 || r.End < r.Start || r.End > len(text) {
				t.Fatalf("invalid span [%d:%d] for len=%d", r.Start, r.End, len(text))
			}
		}
	})
}

func FuzzHasRawHTML(f *testing.F) {
	f.Add("<figure><img src=\"a.png\"></figure>")
	f.Add("```html\n<script>x</script>\n```")
	f.Fuzz(func(_ *testing.T, text string) {
		_ = transclude.HasRawHTML(text)
	})
}

func FuzzReadableHTML(f *testing.F) {
	f.Add("<p>Hello <b>world</b> &amp; <span style='x'>hi</span></p>")
	f.Fuzz(func(_ *testing.T, text string) {
		_ = transclude.ReadableHTML(text)
	})
}

func TestBareHTMLPathsIgnoresMarkdownBackticksAndAvoidsDoubleUnescape(t *testing.T) {

	htmlDoc := "<p>`<img src=\"secret.csv\">` and ```\n<a href=\"other.csv#top\">x</a>\n```</p>" +
		"<!-- <img src=\"comment.png\"> -->" +
		"<pre><code>&lt;img src=\"escaped.png\"&gt;</code></pre>" +
		"<img src=\"a&amp;b.png\">" +
		"<img src=\"double&amp;amp;b.png\">" +
		"<img src=\"app/download.php?f=a/b&amp;name=c.png&amp;storage=1\">" +
		"<a href=\"https://example.com\">ext</a>" +
		"<a href=\"#anchor\">local</a>"

	got := transclude.BareHTMLPaths(htmlDoc)
	want := []string{"secret.csv", "other.csv#top", "a&b.png", "double&amp;b.png"}
	if !slices.Equal(got, want) {
		t.Fatalf("BareHTMLPaths = %#v, want %#v", got, want)
	}

	mdGot := transclude.BarePathsForMode("`<img src=\"secret.csv\">`", 2)
	if len(mdGot) != 0 {
		t.Errorf("Markdown BarePathsForMode should mask inline code, got %#v", mdGot)
	}
	htmlGot := transclude.BarePathsForMode("`<img src=\"secret.csv\">`", 1)
	if !slices.Equal(htmlGot, []string{"secret.csv"}) {
		t.Errorf("HTML BarePathsForMode = %#v, want [secret.csv]", htmlGot)
	}
}

func TestScanMathAndPlanHTMLMasksMathReferences(t *testing.T) {

	dir := t.TempDir()
	for _, name := range []string{"t", "fig.png"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	md := "Formula $x_{[1]}(t)$ and \\(y_{[2]}(t)\\) and price \\$100 to \\$200, plus ![ok](fig.png)\n\n" +
		"$$\nz_{[3]}(t)\n$$\n\n" +
		"`$not_math$`\n"

	spans, escaped := transclude.ScanMath(md)
	if len(spans) != 3 {
		t.Fatalf("ScanMath spans = %+v, want 3", spans)
	}
	if spans[0].Display || spans[1].Display || !spans[2].Display {
		t.Errorf("unexpected Display flags: %+v", spans)
	}
	if len(escaped) != 2 {
		t.Errorf("expected 2 escaped dollars, got %v", escaped)
	}

	refs, err := transclude.PlanHTML(md, dir, nil, nil)
	if err != nil {
		t.Fatalf("PlanHTML: %v", err)
	}
	if len(refs) != 1 || filepath.Base(refs[0].File) != "fig.png" {
		t.Fatalf("PlanHTML refs = %+v, want only fig.png (math [1](t) masked)", refs)
	}

	dollarFile := filepath.Join(dir, "plot$2025$.png")
	if err := os.WriteFile(dollarFile, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	refsDollar, err := transclude.PlanHTML("![photo](plot$2025$.png)\n", dir, nil, nil)
	if err != nil {
		t.Fatalf("PlanHTML dollar dest: %v", err)
	}
	if len(refsDollar) != 1 || filepath.Base(refsDollar[0].File) != "plot$2025$.png" {
		t.Fatalf("PlanHTML dollar dest refs = %+v, want plot$2025$.png", refsDollar)
	}
}

func TestHTMLTagScanUnclosedQuotesMaskPrecedenceAndRawTextRecovery(t *testing.T) {
	refsDQ := transclude.Extract(`<a title="unclosed <img src="after-dq.png">`)
	if len(refsDQ) != 1 || refsDQ[0].Path != "after-dq.png" {
		t.Fatalf("unclosed double-quote recovery = %+v, want [after-dq.png]", refsDQ)
	}

	refsSQ := transclude.Extract(`<a title='unclosed <img src="after-sq.png">`)
	if len(refsSQ) != 1 || refsSQ[0].Path != "after-sq.png" {
		t.Fatalf("unclosed single-quote recovery = %+v, want [after-sq.png]", refsSQ)
	}

	refsClosed := transclude.Extract(`<a title="<img src='nested.png'>">`)
	if len(refsClosed) != 0 {
		t.Fatalf("closed quoted attribute must hide inner img tag, got %+v", refsClosed)
	}

	refsUnquotedLT := transclude.Extract(`<div data=<1 ![hidden](inside.png)>`)
	if len(refsUnquotedLT) != 0 {
		t.Fatalf("unquoted < inside outer tag must keep outer tag match and mask markdown inside, got %+v", refsUnquotedLT)
	}

	if !transclude.HasRawHTML("`<a x=` <b y=`> text`") {
		t.Error("HasRawHTML must detect <b> after inline code masks earlier overlapping <a candidate")
	}
	if transclude.HasRawHTML("<!-- <a x=' -->`<b>`") {
		t.Error("HasRawHTML must mask inline code after comment covers earlier <a candidate")
	}
	if !transclude.HasRawHTML(strings.Repeat(`<a x="<!--">`, 2000)) {
		t.Error("HasRawHTML must detect tags containing literal comment openers without terminator")
	}

	refsAngleCode := transclude.Extract("[x](<code>) ![ok](fig.png)\n")
	if len(refsAngleCode) != 2 || refsAngleCode[0].Path != "code" || refsAngleCode[1].Path != "fig.png" {
		t.Fatalf("[x](<code>) must not mask following content as raw-text, got %+v", refsAngleCode)
	}

	unclosedHTML := strings.Repeat("x <a y ", 2000)
	if refs := transclude.Extract(unclosedHTML); len(refs) != 0 {
		t.Fatalf("repeated unclosed <a should yield 0 refs, got %+v", refs)
	}
	if transclude.HasRawHTML(unclosedHTML) {
		t.Fatal("repeated unclosed <a should not report raw HTML")
	}

	unclosedScript := strings.Repeat("<script ", 2000) + "![ok](fig.png)"
	refsScript := transclude.Extract(unclosedScript)
	if len(refsScript) != 1 || refsScript[0].Path != "fig.png" {
		t.Fatalf("repeated unclosed <script must leave trailing ref intact, got %+v", refsScript)
	}
}
