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
	// Files under hidden paths (as written or via symlink) must never be planned,
	// while a document living inside a hidden parent can still reference siblings.
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

func TestExtractNeverMatchesInsideCodeFenceRapid(t *testing.T) {
	// Masking invariant: arbitrary Markdown links inside a fenced block must
	// never produce extracted references.
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
