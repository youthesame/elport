package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	gosync "sync"
	"sync/atomic"
	"testing"

	"github.com/youthesame/elport/internal/cli"
	"github.com/youthesame/elport/internal/client"
	"github.com/youthesame/elport/internal/frontmatter"
	"github.com/youthesame/elport/internal/state"
	elpsync "github.com/youthesame/elport/internal/sync"
)

func TestCLINewFormatHTMLConfirmRemoteBatchAndCloneRegisteredHTML(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/experiments":
			w.Header().Set("Location", "/api/v2/experiments/77")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 77})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/experiments/77":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           77,
				"title":        "HTML Report",
				"body":         "",
				"content_type": 1,
				"userid":       1,
				"team":         7,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	work := t.TempDir()
	t.Setenv("ELABFTW_BASE_URL", srv.URL)
	t.Setenv("ELABFTW_API_KEY", "test-key")
	env := cli.Env{
		WorkDir:    work,
		ClientOpts: []client.Option{client.WithRootCAs(srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs)},
	}

	docPath := filepath.Join(work, "report.md")
	var out, errBuf bytes.Buffer
	code := cli.RunWithEnv(context.Background(), []string{"new", "--format", "html", "-o", docPath, "HTML Report"}, nil, &out, &errBuf, env)
	if code != cli.ExitOK {
		t.Fatalf("new --format html failed (code=%d): stderr=%q", code, errBuf.String())
	}
	raw, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatal(err)
	}
	meta, _, err := frontmatter.Parse(string(raw))
	if err != nil {
		t.Fatalf("Parse new doc: %v", err)
	}
	if meta.Values["format"] != "html" {
		t.Fatalf("format = %#v, want \"html\"", meta.Values["format"])
	}

	out.Reset()
	errBuf.Reset()
	code = cli.RunWithEnv(context.Background(), []string{"push", "--confirm-remote", "abcdef012345", work}, nil, &out, &errBuf, env)
	if code != cli.ExitError || !strings.Contains(errBuf.String(), "--confirm-remote works on one document at a time") {
		t.Fatalf("expected batch --confirm-remote rejection, got code=%d stderr=%q", code, errBuf.String())
	}

	if err := state.Save(srv.URL, "experiments", "77", map[string]any{
		"content_type": 1,
		"remote_base":  "<p>Hi</p>",
		"local_base":   "Hi\n",
		"path":         docPath,
	}); err != nil {
		t.Fatal(err)
	}
	cloneOut := filepath.Join(work, "other.md")
	out.Reset()
	errBuf.Reset()
	code = cli.RunWithEnv(context.Background(), []string{"clone", "77", "-o", cloneOut}, nil, &out, &errBuf, env)
	if code != cli.ExitError {
		t.Fatalf("expected clone on registered HTML to fail, got code=%d", code)
	}
	if !strings.Contains(errBuf.String(), "published in html format") || !strings.Contains(errBuf.String(), "elport diff") {
		t.Errorf("unexpected clone error for registered HTML: %q", errBuf.String())
	}
	if strings.Contains(errBuf.String(), "delete its base") {
		t.Errorf("clone error for registered HTML must not suggest deleting base: %q", errBuf.String())
	}

	if err := state.Save(srv.URL, "experiments", "77", map[string]any{
		"pending_publish": map[string]any{
			"target_content_type": 1,
			"user":                1,
			"pre_body":            "",
			"pre_content_type":    2,
		},
		"path": docPath,
	}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errBuf.Reset()
	code = cli.RunWithEnv(context.Background(), []string{"clone", "77", "-o", cloneOut}, nil, &out, &errBuf, env)
	if code != cli.ExitError || !strings.Contains(errBuf.String(), "published in html format") || strings.Contains(errBuf.String(), "delete its base") {
		t.Fatalf("expected clone on pending_publish to protect recovery state, got code=%d stderr=%q", code, errBuf.String())
	}
}

func TestCLINewFormatHTMLOverlapsFirstPush(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	var mu gosync.Mutex
	body := ""
	ct := 1
	newInGet := make(chan struct{})
	releaseNewGet := make(chan struct{})
	var firstGetOnce gosync.Once
	var meCalls atomic.Int32

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/experiments":
			w.Header().Set("Location", "/api/v2/experiments/78")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 78})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/experiments/78":
			paused := false
			firstGetOnce.Do(func() {
				paused = true
				close(newInGet)
			})
			if paused {
				<-releaseNewGet
			}
			mu.Lock()
			resp := map[string]any{
				"id":           78,
				"title":        "Overlap",
				"body":         body,
				"content_type": ct,
				"userid":       1,
				"team":         7,
				"uploads":      []any{},
			}
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(resp)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/users/me":
			meCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"userid":    1,
				"team":      7,
				"usergroup": 4,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/experiments/78/uploads":
			_ = json.NewEncoder(w).Encode([]any{})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v2/experiments/78":
			var patch map[string]any
			_ = json.NewDecoder(r.Body).Decode(&patch)
			mu.Lock()
			if b, ok := patch["body"].(string); ok {
				body = b
			}
			if c, ok := patch["content_type"].(float64); ok {
				ct = int(c)
			}
			resp := map[string]any{
				"id":           78,
				"title":        "Overlap",
				"body":         body,
				"content_type": ct,
				"userid":       1,
				"team":         7,
				"uploads":      []any{},
			}
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	work := t.TempDir()
	t.Setenv("ELABFTW_BASE_URL", srv.URL)
	t.Setenv("ELABFTW_API_KEY", "test-key")
	env := cli.Env{
		WorkDir:    work,
		ClientOpts: []client.Option{client.WithRootCAs(srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs)},
	}

	contended := make(chan struct{})
	var contendOnce gosync.Once
	restoreContend := state.OnLockContendForTest(func() {
		contendOnce.Do(func() { close(contended) })
	})
	defer restoreContend()

	docPath := filepath.Join(work, "overlap.md")
	var newOut, newErr bytes.Buffer
	newDone := make(chan int, 1)
	go func() {
		newDone <- cli.RunWithEnv(context.Background(), []string{"new", "--format", "html", "-o", docPath, "Overlap"}, nil, &newOut, &newErr, env)
	}()

	select {
	case <-newInGet:
	case code := <-newDone:
		t.Fatalf("new --format html exited before GET (code=%d): stderr=%q", code, newErr.String())
	}
	raw, err := os.ReadFile(docPath)
	if err != nil {
		close(releaseNewGet)
		<-newDone
		t.Fatalf("read doc written by new: %v", err)
	}
	meta, _, err := frontmatter.Parse(string(raw))
	if err != nil {
		close(releaseNewGet)
		<-newDone
		t.Fatalf("parse doc written by new: %v", err)
	}
	if meta.Values["id"] != 78 || meta.Values["format"] != "html" {
		close(releaseNewGet)
		<-newDone
		t.Fatalf("expected new to expose id=78 and format=html before GET, got %#v", meta.Values)
	}
	if err := os.WriteFile(docPath, []byte(frontmatter.Render(meta, "Concurrent HTML body\n")), 0o644); err != nil {
		close(releaseNewGet)
		<-newDone
		t.Fatalf("write body before concurrent push: %v", err)
	}

	var pushOut, pushErr bytes.Buffer
	pushDone := make(chan int, 1)
	go func() {
		pushDone <- cli.RunWithEnv(context.Background(), []string{"push", docPath}, nil, &pushOut, &pushErr, env)
	}()

	select {
	case <-contended:
	case code := <-pushDone:
		close(releaseNewGet)
		<-newDone
		t.Fatalf("first push must block behind new --format html lock, finished early with code=%d stderr=%q", code, pushErr.String())
	}
	if got := meCalls.Load(); got != 0 {
		close(releaseNewGet)
		<-newDone
		<-pushDone
		t.Fatalf("push must not contact server while new holds entity lock, got %d /users/me call(s)", got)
	}
	close(releaseNewGet)

	if code := <-newDone; code != cli.ExitOK {
		t.Fatalf("new --format html failed (code=%d): stderr=%q", code, newErr.String())
	}
	if code := <-pushDone; code != cli.ExitOK {
		t.Fatalf("first push failed (code=%d): stderr=%q", code, pushErr.String())
	}
	saved, err := state.Load(srv.URL, "experiments", "78")
	if err != nil || !elpsync.IsRegisteredHTML(saved) {
		t.Fatalf("registered HTML state must survive overlapping new --format html, got %#v (err=%v)", saved, err)
	}
}

func TestCLIPhase2TransitionsAndMetadataPull(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	var mu gosync.Mutex
	body := "Initial **md**\n"
	ct := 2
	categoryTitle := "Physics"
	tags := "remote-tag"

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/users/me":
			_ = json.NewEncoder(w).Encode(map[string]any{"userid": 1, "team": 7})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/teams/7/experiments_categories":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 3, "title": "Physics"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/experiments/90":
			mu.Lock()
			resp := map[string]any{
				"id":             90,
				"title":          "Phase 2 CLI",
				"body":           body,
				"content_type":   ct,
				"category_title": categoryTitle,
				"tags":           tags,
				"userid":         1,
				"team":           7,
				"uploads":        []any{},
			}
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(resp)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v2/experiments/90":
			var patch map[string]any
			_ = json.NewDecoder(r.Body).Decode(&patch)
			mu.Lock()
			if b, ok := patch["body"].(string); ok {
				body = b
			}
			if c, ok := patch["content_type"].(float64); ok {
				ct = int(c)
			}
			resp := map[string]any{
				"id":             90,
				"title":          "Phase 2 CLI",
				"body":           body,
				"content_type":   ct,
				"category_title": categoryTitle,
				"tags":           tags,
				"userid":         1,
				"team":           7,
				"uploads":        []any{},
			}
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	work := t.TempDir()
	t.Setenv("ELABFTW_BASE_URL", srv.URL)
	t.Setenv("ELABFTW_API_KEY", "test-key")
	env := cli.Env{
		WorkDir:    work,
		ClientOpts: []client.Option{client.WithRootCAs(srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs)},
	}

	docPath := filepath.Join(work, "report.md")
	if err := os.WriteFile(docPath, []byte("---\nid: 90\nentity: experiments\nprofile: env\nformat: html\ntitle: Phase 2 CLI\n---\n\nInitial **md**\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(srv.URL, "experiments", "90", map[string]any{
		"remote_base":  "Initial **md**\n",
		"local_base":   "Initial **md**\n",
		"content_type": 2,
		"meta_base":    map[string]any{"title": "Phase 2 CLI"},
		"path":         docPath,
	}); err != nil {
		t.Fatal(err)
	}

	var out, errBuf bytes.Buffer
	if code := cli.RunWithEnv(context.Background(), []string{"push", docPath}, nil, &out, &errBuf, env); code != cli.ExitOK {
		t.Fatalf("2->1 push failed (code=%d): out=%q err=%q", code, out.String(), errBuf.String())
	}

	out.Reset()
	errBuf.Reset()
	if code := cli.RunWithEnv(context.Background(), []string{"pull", docPath}, nil, &out, &errBuf, env); code != cli.ExitOK {
		t.Fatalf("metadata-only pull failed (code=%d): out=%q err=%q", code, out.String(), errBuf.String())
	}
	raw, _ := os.ReadFile(docPath)
	meta, localBody, err := frontmatter.Parse(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if meta.Values["category"] != "Physics" || localBody != "Initial **md**\n" {
		t.Fatalf("unexpected doc after metadata pull: meta=%#v body=%q", meta.Values, localBody)
	}

	home2 := t.TempDir()
	t.Setenv("HOME", home2)
	t.Setenv("USERPROFILE", home2)

	mu.Lock()
	hCur := elpsync.ShortHash(elpsync.BodySHA256(body))
	mu.Unlock()

	out.Reset()
	errBuf.Reset()
	if code := cli.RunWithEnv(context.Background(), []string{"push", "--confirm-remote", hCur, docPath}, nil, &out, &errBuf, env); code != cli.ExitOK {
		t.Fatalf("second-machine adoption failed (code=%d): out=%q err=%q", code, out.String(), errBuf.String())
	}
	if !strings.Contains(out.String(), "confirmed: "+srv.URL+" experiments/90 (html → html)") {
		t.Errorf("adoption output missing confirmation details: %q", out.String())
	}

	reverted := strings.Replace(string(raw), "format: html", "format: markdown", 1)
	if err := os.WriteFile(docPath, []byte(reverted), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errBuf.Reset()
	if code := cli.RunWithEnv(context.Background(), []string{"push", docPath}, nil, &out, &errBuf, env); code != cli.ExitOK {
		t.Fatalf("1->2 revert failed (code=%d): out=%q err=%q", code, out.String(), errBuf.String())
	}
	if !strings.Contains(out.String(), "body updated (markdown)") {
		t.Errorf("unexpected 1->2 revert stdout: %q", out.String())
	}
}
