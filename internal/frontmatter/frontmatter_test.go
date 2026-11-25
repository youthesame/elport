package frontmatter_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"pgregory.net/rapid"

	"github.com/youthesame/elport/internal/frontmatter"
)

type fmFixture struct {
	Parse []struct {
		Name     string         `json:"name"`
		Input    string         `json:"input"`
		Meta     map[string]any `json:"meta"`
		KeyOrder []string       `json:"key_order"`
		Body     string         `json:"body"`
		Error    string         `json:"error"`
	} `json:"parse"`
	Render []struct {
		Name     string         `json:"name"`
		Meta     map[string]any `json:"meta"`
		KeyOrder []string       `json:"key_order"`
		Body     string         `json:"body"`
		Output   string         `json:"output"`
	} `json:"render"`
	TextMeta []struct {
		Name  string         `json:"name"`
		Input string         `json:"input"`
		Meta  map[string]any `json:"meta"`
	} `json:"text_meta"`
	ParseServerID []struct {
		Name  string `json:"name"`
		Input any    `json:"input"`
		ID    int    `json:"id"`
		Error string `json:"error"`
	} `json:"parse_server_id"`
}

func normalizeJSONNumbers(v any) any {
	switch x := v.(type) {
	case float64:
		if float64(int(x)) == x {
			return int(x)
		}
		return x
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = normalizeJSONNumbers(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = normalizeJSONNumbers(val)
		}
		return out
	case []string:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = val
		}
		return out
	default:
		return v
	}
}

func loadFixture(t *testing.T) fmFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", "frontmatter.json"))
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	var fx fmFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatalf("unmarshal golden fixture: %v", err)
	}
	return fx
}

func TestFrontmatterGoldenParse(t *testing.T) {
	fx := loadFixture(t)
	for _, tc := range fx.Parse {
		t.Run(tc.Name, func(t *testing.T) {
			meta, body, err := frontmatter.Parse(tc.Input)
			if tc.Error != "" {
				if err == nil {
					t.Fatalf("expected error %q, got meta=%v body=%q", tc.Error, meta.Values, body)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if body != tc.Body {
				t.Errorf("body = %q, want %q", body, tc.Body)
			}
			if !frontmatter.KeysEqual(meta.Order, tc.KeyOrder) {
				t.Errorf("key_order = %v, want %v", meta.Order, tc.KeyOrder)
			}
			gotMeta := normalizeJSONNumbers(meta.Values)
			wantMeta := normalizeJSONNumbers(tc.Meta)
			if !reflect.DeepEqual(gotMeta, wantMeta) {
				t.Errorf("meta = %#v, want %#v", gotMeta, wantMeta)
			}
		})
	}
}

func TestFrontmatterGoldenRender(t *testing.T) {
	fx := loadFixture(t)
	for _, tc := range fx.Render {
		t.Run(tc.Name, func(t *testing.T) {
			meta := frontmatter.NewMeta()
			for _, k := range tc.KeyOrder {
				meta.Set(k, normalizeJSONNumbers(tc.Meta[k]))
			}
			got := frontmatter.Render(meta, tc.Body)
			if got != tc.Output {
				t.Errorf("Render mismatch:\ngot:\n%q\nwant:\n%q", got, tc.Output)
			}
		})
	}
}

func TestFrontmatterGoldenTextMeta(t *testing.T) {
	fx := loadFixture(t)
	for _, tc := range fx.TextMeta {
		t.Run(tc.Name, func(t *testing.T) {
			got, ok := frontmatter.TextMeta(tc.Input)
			if tc.Meta == nil {
				if ok {
					t.Fatalf("expected nil meta, got %#v", got.Values)
				}
				return
			}
			if !ok {
				t.Fatalf("expected meta %#v, got ok=false", tc.Meta)
			}
			if !reflect.DeepEqual(normalizeJSONNumbers(got.Values), normalizeJSONNumbers(tc.Meta)) {
				t.Errorf("meta = %#v, want %#v", got.Values, tc.Meta)
			}
		})
	}
}

func TestFrontmatterGoldenParseServerID(t *testing.T) {
	fx := loadFixture(t)
	for _, tc := range fx.ParseServerID {
		t.Run(tc.Name, func(t *testing.T) {
			input := normalizeJSONNumbers(tc.Input)
			got, err := frontmatter.ParseServerID(input)
			if tc.Error != "" {
				if err == nil {
					t.Fatalf("expected error %q, got %d", tc.Error, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.ID {
				t.Errorf("got %d, want %d", got, tc.ID)
			}
		})
	}
}

func TestFrontmatterRenderParseRoundTripRapid(t *testing.T) {

	rapid.Check(t, func(t *rapid.T) {
		id := rapid.IntRange(1, 100000).Draw(t, "id")
		title := rapid.StringMatching(`[A-Za-z0-9 _:#'"\n\t\x00日本語]{1,30}`).Draw(t, "title")
		profile := rapid.StringMatching(`[A-Za-z0-9_-]{1,12}`).Draw(t, "profile")
		body := rapid.StringMatching(`[\nA-Za-z0-9 日本語._-]{0,40}`).Draw(t, "body")

		meta := frontmatter.NewMeta()
		meta.Set("id", id)
		meta.Set("profile", profile)
		meta.Set("title", title)

		rendered := frontmatter.Render(meta, body)
		parsed, parsedBody, err := frontmatter.Parse(rendered)
		if err != nil {
			t.Fatalf("Parse(Render(...)) failed: %v\nrendered=%q", err, rendered)
		}
		if parsedBody != body {
			t.Fatalf("body mismatch: got %q, want %q", parsedBody, body)
		}
		if parsed.Values["id"] != id || parsed.Values["profile"] != profile || parsed.Values["title"] != title {
			t.Fatalf("meta mismatch: got %#v, want id=%d profile=%q title=%q", parsed.Values, id, profile, title)
		}
	})
}

func TestFrontmatterYAML11EdgeCasesAndUTF8Validation(t *testing.T) {

	if _, _, err := frontmatter.Parse("---\nid: 08\n---\nBody\n"); err == nil {
		t.Error("expected id: 08 to be rejected as string, not int")
	}
	meta, _, err := frontmatter.Parse("---\nattachments: 08\n---\nBody\n")
	if err != nil {
		t.Fatalf("attachments: 08 failed: %v", err)
	}
	if !reflect.DeepEqual(meta.Values["attachments"], []string{"08"}) {
		t.Errorf("attachments: 08 = %#v, want [\"08\"]", meta.Values["attachments"])
	}
	if _, _, err := frontmatter.Parse("---\nattachments: !!binary ZGF0YS5jc3Y=\n---\nBody\n"); err == nil {
		t.Error("expected !!binary in attachments to be rejected")
	}
	if _, _, err := frontmatter.Parse("---\ntrue: 1\n1: 2\n---\nBody\n"); err == nil {
		t.Error("expected duplicate key between true and 1 to be rejected")
	}
	setDoc := "---\n!!set {a: null}\n---\nBody\n"
	m, b, err := frontmatter.Parse(setDoc)
	if err != nil || m.Len() != 0 || b != setDoc {
		t.Errorf("top-level !!set = (%v, %q, %v), want empty meta and full body", m.Values, b, err)
	}
	if _, _, err := frontmatter.Parse("---\ntags: !!set {a: null}\n---\nBody\n"); err == nil {
		t.Error("expected tags: !!set to be rejected")
	}

	dir := t.TempDir()
	badUTF8 := filepath.Join(dir, "bad.md")
	if err := os.WriteFile(badUTF8, []byte("---\nid: 5\nprofile: lab\n---\n\xff\xfe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := frontmatter.DocumentMeta(badUTF8); ok {
		t.Error("DocumentMeta must reject files with invalid UTF-8 bytes in body")
	}

	for _, profile := range []string{"0E0_", "1_0.5"} {
		meta := frontmatter.NewMeta()
		meta.Set("id", 1)
		meta.Set("profile", profile)
		rendered := frontmatter.Render(meta, "")
		parsed, _, err := frontmatter.Parse(rendered)
		if err != nil {
			t.Fatalf("Parse(Render(%q)) failed: %v\nrendered=%q", profile, err, rendered)
		}
		if parsed.Values["profile"] != profile {
			t.Errorf("profile round-trip for %q = %#v (rendered=%q)", profile, parsed.Values["profile"], rendered)
		}
	}
}

func TestFrontmatterFormatKey(t *testing.T) {

	for _, val := range []string{"markdown", "html"} {
		doc := "---\nid: 7\nformat: " + val + "\n---\nBody\n"
		meta, body, err := frontmatter.Parse(doc)
		if err != nil {
			t.Fatalf("Parse format=%s failed: %v", val, err)
		}
		if meta.Values["format"] != val {
			t.Errorf("format = %#v, want %q", meta.Values["format"], val)
		}
		if body != "Body\n" {
			t.Errorf("body = %q, want %q", body, "Body\n")
		}
		rendered := frontmatter.Render(meta, body)
		reparsed, _, err := frontmatter.Parse(rendered)
		if err != nil {
			t.Fatalf("reparse rendered format=%s failed: %v", val, err)
		}
		if reparsed.Values["format"] != val {
			t.Errorf("reparsed format = %#v, want %q", reparsed.Values["format"], val)
		}
	}

	for _, bad := range []string{"HTML", "md", "1", "true", "null", "[html]"} {
		doc := "---\nformat: " + bad + "\n---\nBody\n"
		if _, _, err := frontmatter.Parse(doc); err == nil {
			t.Errorf("expected format: %s to be rejected", bad)
		}
	}
}

func TestFrontmatterRawBodyPreservesCRLF(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "crlf frontmatter and body with blank separator",
			in:   "---\r\nid: 42\r\nformat: html\r\n---\r\n\r\nLine 1\r\nLine 2\r\n",
			want: "Line 1\r\nLine 2\r\n",
		},
		{
			name: "utf8 bom with crlf body and no blank separator",
			in:   "\ufeff---\r\nid: 42\r\n---\r\nLine 1\r\nLine 2",
			want: "Line 1\r\nLine 2",
		},
		{
			name: "extra blank line preserved",
			in:   "---\r\nid: 42\r\n---\r\n\r\n\r\nBody\r\n",
			want: "\r\nBody\r\n",
		},
		{
			name: "no frontmatter returns original",
			in:   "Plain\r\nBody\r\n",
			want: "Plain\r\nBody\r\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := frontmatter.RawBody(tc.in); got != tc.want {
				t.Errorf("RawBody(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
