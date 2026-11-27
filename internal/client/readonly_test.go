package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/youthesame/elport/internal/client"
	"github.com/youthesame/elport/internal/plan"
)

func TestReadOnlyClientAllowsGetsAndRefusesWritesBeforeHTTP(t *testing.T) {
	var gets, writes atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes.Add(1)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
			return
		}
		gets.Add(1)
		switch {
		case strings.HasSuffix(r.URL.Path, "/uploads/3"):
			_, _ = w.Write([]byte("bin"))
		case r.URL.Path == "/api/v2/users/me" || r.URL.Path == "/api/v2/info" || r.URL.Path == "/api/v2/experiments/1" || strings.HasSuffix(r.URL.Path, "/revisions/5"):
			_, _ = w.Write([]byte(`{"id":1}`))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()

	c := tlsClientForServer(t, srv, "1-key", client.WithReadOnly(true))
	if !c.ReadOnly() {
		t.Fatal("ReadOnly() must report true")
	}
	ctx := context.Background()
	reads := map[string]func() error{
		"Get":       func() error { _, err := c.Get(ctx, "experiments", 1); return err },
		"GetRaw":    func() error { _, err := c.GetRaw(ctx, "experiments", 1); return err },
		"Uploads":   func() error { _, err := c.Uploads(ctx, "experiments", 1); return err },
		"Revisions": func() error { _, err := c.Revisions(ctx, "experiments", 1); return err },
		"Revision":  func() error { _, err := c.Revision(ctx, "experiments", 1, 5); return err },
		"Search":    func() error { _, err := c.Search(ctx, "experiments", url.Values{}); return err },
		"Download":  func() error { _, err := c.Download(ctx, "experiments", 1, 3); return err },
		"Me":        func() error { _, err := c.Me(ctx); return err },
		"Info":      func() error { _, err := c.Info(ctx); return err },
		"APIKeys":   func() error { _, err := c.APIKeys(ctx); return err },
		"Comments":  func() error { _, err := c.Comments(ctx, "experiments", 1); return err },
	}
	for name, fn := range reads {
		if err := fn(); err != nil {
			t.Errorf("%s must be allowed in read-only mode: %v", name, err)
		}
	}
	if int(gets.Load()) != len(reads) {
		t.Fatalf("expected %d GETs, got %d", len(reads), gets.Load())
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "data.csv"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	approved, err := plan.Approve("[d](data.csv)\n", dir, nil, nil, nil, nil, nil)
	if err != nil || len(approved.Files) != 1 {
		t.Fatalf("Approve: %v", err)
	}
	refused := map[string]func() error{
		"Create":     func() error { _, err := c.Create(ctx, "experiments", "t"); return err },
		"Patch":      func() error { _, err := c.Patch(ctx, "experiments", 1, map[string]any{"body": "x"}); return err },
		"Upload":     func() error { _, err := c.Upload(ctx, "experiments", 1, approved.Files[0]); return err },
		"AddTag":     func() error { return c.AddTag(ctx, "experiments", 1, "x") },
		"AddLink":    func() error { return c.AddLink(ctx, "experiments", 1, "items", 2) },
		"AddComment": func() error { return c.AddComment(ctx, "experiments", 1, "hi") },
	}
	for name, fn := range refused {
		err := fn()
		if err == nil || !strings.Contains(err.Error(), "read-only mode is enabled") || !strings.HasPrefix(err.Error(), "refusing ") {
			t.Errorf("%s must be refused by read-only mode, got %v", name, err)
		}
	}
	if writes.Load() != 0 {
		t.Fatalf("read-only client sent %d write requests", writes.Load())
	}

	writable := tlsClientForServer(t, srv, "1-key")
	if writable.ReadOnly() {
		t.Fatal("default client must be writable")
	}
	if err := writable.AddTag(ctx, "experiments", 1, "x"); err != nil || writes.Load() != 1 {
		t.Fatalf("writable client must send writes: %v (writes=%d)", err, writes.Load())
	}
}

func TestGetRawPreservesServerShapeAndRevisionsPaths(t *testing.T) {
	var paths []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/api/v2/items/7":
			_, _ = w.Write([]byte(`{"id":7,"body":null,"future_field":{"x":1.50},"uploads":[]}`))
		case "/api/v2/items/7/revisions":
			_, _ = w.Write([]byte(`[{"id":3,"created_at":"2026-11-01","fullname":"Ada"}]`))
		case "/api/v2/items/7/revisions/3":
			_, _ = w.Write([]byte(`{"id":3,"body":null,"content_type":1}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := tlsClientForServer(t, srv, "1-key")
	ctx := context.Background()

	raw, err := c.GetRaw(ctx, "items", 7)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := raw["body"]; !ok || v != nil {
		t.Fatalf("GetRaw must keep body null, got %#v", raw["body"])
	}
	extra, ok := raw["future_field"].(map[string]any)
	if !ok || extra["x"] != json.Number("1.50") {
		t.Fatalf("GetRaw must keep unknown fields verbatim, got %#v", raw["future_field"])
	}
	normalized, err := c.Get(ctx, "items", 7)
	if err != nil || normalized["body"] != "" {
		t.Fatalf("Get must still normalize body: %#v %v", normalized["body"], err)
	}

	revs, err := c.Revisions(ctx, "items", 7)
	if err != nil || len(revs) != 1 || revs[0]["id"] != json.Number("3") {
		t.Fatalf("Revisions: %#v %v", revs, err)
	}
	rev, err := c.Revision(ctx, "items", 7, 3)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := rev["body"]; !ok || v != nil {
		t.Fatalf("Revision must not normalize body, got %#v", rev["body"])
	}
	before := len(paths)
	for _, bad := range []int{0, -1} {
		if _, err := c.Revision(ctx, "items", 7, bad); err == nil {
			t.Errorf("Revision(%d) must be rejected", bad)
		}
	}
	if len(paths) != before {
		t.Fatal("invalid revision id must not reach the server")
	}
	want := []string{"/api/v2/items/7", "/api/v2/items/7", "/api/v2/items/7/revisions", "/api/v2/items/7/revisions/3"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
}
