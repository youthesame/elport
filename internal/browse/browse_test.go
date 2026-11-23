package browse_test

import (
	"bytes"
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/youthesame/elport/internal/browse"
)

type fakeSearcher struct {
	pages  [][]map[string]any
	calls  []url.Values
	entity string
}

func (f *fakeSearcher) Search(_ context.Context, entity string, params url.Values) ([]map[string]any, error) {
	f.entity = entity
	f.calls = append(f.calls, params)
	idx := len(f.calls) - 1
	if idx >= len(f.pages) {
		return nil, nil
	}
	return f.pages[idx], nil
}

func TestEntitiesTableJSONTruncationAndPagination(t *testing.T) {
	limit := 2
	fs := &fakeSearcher{
		pages: [][]map[string]any{
			{
				{"id": 9, "date": "2026-11-20", "modified_at": "2026-11-22 10:00", "title": "First"},
				{"id": 120, "date": "2026-11-21", "modified_at": "2026-11-23 12:00", "title": "Second"},
			},
		},
	}
	var out, errBuf bytes.Buffer
	err := browse.Entities(context.Background(), fs, browse.ListOptions{
		Entity: "experiments",
		Scope:  "team",
		Limit:  &limit,
		Order:  "lastchange",
		Tags:   []string{"a,b", "c"},
	}, &out, &errBuf)
	if err != nil {
		t.Fatalf("Entities: %v", err)
	}
	wantOut := "9    2026-11-22 10:00  First\n120  2026-11-23 12:00  Second\n"
	if out.String() != wantOut {
		t.Errorf("stdout =\n%q\nwant:\n%q", out.String(), wantOut)
	}
	if !strings.Contains(errBuf.String(), "showing 2; raise --limit") {
		t.Errorf("expected truncation notice on stderr, got %q", errBuf.String())
	}
	if gotTags := fs.calls[0]["tags[]"]; len(gotTags) != 2 || gotTags[0] != "a,b" || gotTags[1] != "c" {
		t.Errorf("tags[] params = %v, want [a,b c]", gotTags)
	}

	paged := &fakeSearcher{
		pages: [][]map[string]any{
			{{"id": 1, "title": "A"}, {"id": 2, "title": "B"}},
			{{"id": 3, "title": "C"}},
			{},
		},
	}
	out.Reset()
	errBuf.Reset()
	err = browse.Entities(context.Background(), paged, browse.ListOptions{
		Entity: "items",
		Scope:  "self",
		Limit:  nil,
		AsJSON: true,
	}, &out, &errBuf)
	if err != nil {
		t.Fatalf("Entities --all --json: %v", err)
	}
	if len(paged.calls) != 3 || paged.calls[1].Get("offset") != "2" || paged.calls[2].Get("offset") != "3" {
		t.Fatalf("unexpected paging calls: %+v", paged.calls)
	}
	if !strings.Contains(out.String(), `"title": "C"`) || errBuf.Len() != 0 {
		t.Errorf("unexpected --all --json output: stdout=%q stderr=%q", out.String(), errBuf.String())
	}
}

func TestEntitiesValidationErrors(t *testing.T) {
	zero := 0
	fs := &fakeSearcher{}
	var out, errBuf bytes.Buffer
	if err := browse.Entities(context.Background(), fs, browse.ListOptions{Entity: "experiments", Scope: "self", Limit: &zero}, &out, &errBuf); err == nil {
		t.Error("expected error for limit=0")
	}
	if err := browse.Entities(context.Background(), fs, browse.ListOptions{Entity: "experiments", Scope: "self", Offset: -1}, &out, &errBuf); err == nil {
		t.Error("expected error for offset=-1")
	}
}

func TestActivityHeatmapMatchesFormat(t *testing.T) {
	today := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	rows := []map[string]any{
		{"created_at": "2026-11-23 09:00:00", "userid": 1, "fullname": "Ada Lovelace"},
		{"created_at": "2026-11-23 11:00:00", "userid": 1, "fullname": "Ada Lovelace"},
		{"created_at": "2026-11-20 15:00:00", "userid": 1, "fullname": "Ada Lovelace"},
	}
	out := browse.Activity(rows, "experiments", today, false, 20)
	for _, want := range []string{
		"experiments created per day · ",
		"Ada Lovelace  3 experiments · 2 active days",
		"Mon ",
		"· 0  ░ 1  ▒ 2  ▓ 3  █ 4+",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Activity output missing %q:\n%s", want, out)
		}
	}

	empty := browse.Activity(nil, "experiments", today, false, 20)
	if !strings.HasPrefix(empty, "no experiments created since ") {
		t.Errorf("empty Activity = %q", empty)
	}
}
