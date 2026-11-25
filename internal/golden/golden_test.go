package golden_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type frontmatterFixture struct {
	Parse         []json.RawMessage `json:"parse"`
	Render        []json.RawMessage `json:"render"`
	TextMeta      []json.RawMessage `json:"text_meta"`
	ParseServerID []json.RawMessage `json:"parse_server_id"`
}

type transcludeFixture struct {
	Extract      []json.RawMessage `json:"extract"`
	HasRawHTML   []json.RawMessage `json:"has_raw_html"`
	BarePaths    []json.RawMessage `json:"bare_paths"`
	Ignore       []json.RawMessage `json:"ignore"`
	DownloadURL  []json.RawMessage `json:"download_url"`
	ReadableHTML []json.RawMessage `json:"readable_html"`
	NameUploads  struct {
		Input  []json.RawMessage `json:"input"`
		Output []json.RawMessage `json:"output"`
	} `json:"name_uploads"`
	Reverse struct {
		BaseURL   string   `json:"base_url"`
		Input     string   `json:"input"`
		Output    string   `json:"output"`
		PlacedIDs []int    `json:"placed_ids"`
		Unmatched []string `json:"unmatched"`
	} `json:"reverse"`
	Relink struct {
		Input  string `json:"input"`
		Output string `json:"output"`
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
}

func TestGoldenFixturesLoadCleanly(t *testing.T) {

	fmBytes, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", "frontmatter.json"))
	if err != nil {
		t.Fatalf("read frontmatter.json: %v", err)
	}
	var fm frontmatterFixture
	if err := json.Unmarshal(fmBytes, &fm); err != nil {
		t.Fatalf("unmarshal frontmatter.json: %v", err)
	}
	if len(fm.Parse) == 0 || len(fm.Render) == 0 || len(fm.TextMeta) == 0 || len(fm.ParseServerID) == 0 {
		t.Fatalf("frontmatter.json missing cases: parse=%d render=%d text_meta=%d server_id=%d",
			len(fm.Parse), len(fm.Render), len(fm.TextMeta), len(fm.ParseServerID))
	}

	trBytes, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", "transclude.json"))
	if err != nil {
		t.Fatalf("read transclude.json: %v", err)
	}
	var tr transcludeFixture
	if err := json.Unmarshal(trBytes, &tr); err != nil {
		t.Fatalf("unmarshal transclude.json: %v", err)
	}
	if len(tr.Extract) == 0 ||
		len(tr.HasRawHTML) == 0 ||
		len(tr.BarePaths) == 0 ||
		len(tr.Ignore) == 0 ||
		len(tr.DownloadURL) == 0 ||
		len(tr.ReadableHTML) == 0 ||
		len(tr.NameUploads.Input) == 0 ||
		len(tr.NameUploads.Output) == 0 ||
		tr.Reverse.Input == "" ||
		tr.Reverse.Output == "" ||
		len(tr.Reverse.PlacedIDs) == 0 ||
		len(tr.Reverse.Unmatched) == 0 ||
		tr.Relink.Input == "" ||
		tr.Relink.Output == "" ||
		tr.AbsoluteLinks.Input == "" ||
		tr.AbsoluteLinks.Output == "" ||
		len(tr.SafeName.POSIX) == 0 ||
		len(tr.SafeName.Windows) == 0 {
		t.Fatalf("transclude.json missing one or more required sections")
	}
}
