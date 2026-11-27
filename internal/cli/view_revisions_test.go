package cli_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/youthesame/elport/internal/cli"
)

func TestViewJSONEmitsRawEntityWithOneRequest(t *testing.T) {
	f := newFakeELab(t)
	env := f.env(t.TempDir(), false)
	f.raw["GET items/5"] = `{"id":5,"body":null,"title":"<b>&</b>","metadata":"{\"extra_fields\":{}}","compounds_links":[{"id":1}],"uploads":[{"id":9,"real_name":"a.csv"}],"future":1.50}`

	code, stdout, stderr := run(env, "view", "--json", "--entity", "items", "5")
	if code != cli.ExitOK {
		t.Fatalf("view --json: %d %q", code, stderr)
	}
	if got := f.reset(); len(got) != 1 || got[0] != "GET /api/v2/items/5" {
		t.Fatalf("view --json must issue exactly one GET, got %v", got)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if v, ok := got["body"]; !ok || v != nil {
		t.Fatalf("body must stay null: %#v", got["body"])
	}
	for _, key := range []string{"metadata", "compounds_links", "uploads"} {
		if _, ok := got[key]; !ok {
			t.Errorf("view --json dropped %s", key)
		}
	}
	for _, want := range []string{`"title": "<b>&</b>"`, `"future": 1.50`, "\n  \"id\": 5"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

func TestViewFlagValidation(t *testing.T) {
	f := newFakeELab(t)
	env := f.env(t.TempDir(), false)
	for _, args := range [][]string{
		{"view", "--json", "-o", "out", "5"},
		{"view", "--json", "--attachment", "a.csv", "5"},
		{"view", "--attachment", "a.csv", "-o", "out", "5"},
		{"view", "--attachment=", "5"},
		{"view", "-o", "", "5"},
	} {
		if code, _, _ := run(env, args...); code != cli.ExitUsage {
			t.Errorf("%v: exit = %d, want %d", args, code, cli.ExitUsage)
		}
	}
	if got := f.reset(); len(got) != 0 {
		t.Fatalf("usage errors must not reach the server: %v", got)
	}
}

func sha(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

func TestViewAttachmentWritesVerifiedBytesToStdout(t *testing.T) {
	f := newFakeELab(t)
	work := t.TempDir()
	env := f.env(work, false)
	f.raw["GET experiments/42/uploads"] = `[
		{"id":12,"real_name":"image.png","hash_algorithm":"sha256","hash":"` + sha("second") + `"},
		{"id":11,"real_name":"Image.PNG","hash_algorithm":"sha256","hash":"` + sha("first\x1b[2J") + `"},
		{"id":13,"real_name":".secret","hash_algorithm":"sha256","hash":"` + sha("hidden") + `"},
		{"id":14,"real_name":"bad.bin","hash_algorithm":"sha256","hash":"` + sha("expected") + `"},
		{"id":15,"real_name":"nohash.txt"}
	]`
	f.raw["GET experiments/42/uploads/11"] = "first\x1b[2J"
	f.raw["GET experiments/42/uploads/12"] = "second"
	f.raw["GET experiments/42/uploads/13"] = "hidden"
	f.raw["GET experiments/42/uploads/14"] = "tampered"
	f.raw["GET experiments/42/uploads/15"] = "plain"

	for _, tc := range []struct{ name, want string }{
		{"image.png", "first\x1b[2J"},
		{"IMAGE.png", "first\x1b[2J"},
		{"image-12.png", "second"},
		{"nohash.txt", "plain"},
	} {
		code, stdout, stderr := run(env, "view", "42", "--attachment", tc.name)
		if code != cli.ExitOK || stdout != tc.want {
			t.Errorf("--attachment %s: code=%d stdout=%q stderr=%q", tc.name, code, stdout, stderr)
		}
	}
	f.reset()

	code, stdout, stderr := run(env, "view", "42", "--attachment", ".secret")
	if code != cli.ExitError || stdout != "" || !strings.Contains(stderr, "available: Image.PNG, bad.bin, image-12.png, nohash.txt") {
		t.Fatalf("hidden attachment must miss with the selectable names: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	for _, r := range f.reset() {
		if strings.Contains(r, "/uploads/") {
			t.Fatalf("a miss must not download anything: %s", r)
		}
	}

	code, stdout, stderr = run(env, "view", "42", "--attachment", "bad.bin")
	if code != cli.ExitError || stdout != "" || !strings.Contains(stderr, "SHA-256") {
		t.Fatalf("hash mismatch must fail before writing: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	f.raw["GET experiments/43/uploads"] = `[]`
	code, _, stderr = run(env, "view", "43", "--attachment", "x")
	if code != cli.ExitError || !strings.Contains(stderr, "has no attachments") {
		t.Fatalf("empty list: code=%d stderr=%q", code, stderr)
	}

	f.reset()
	code, stdout, stderr = run(f.env(work, true), "view", "42", "--attachment", "image.png")
	if code != cli.ExitError || stdout != "" || !strings.Contains(stderr, "refusing to write attachment to a terminal") {
		t.Fatalf("terminal stdout must be refused: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if got := f.reset(); len(got) != 0 {
		t.Fatalf("terminal refusal must not reach the server: %v", got)
	}
	if names := dirNames(t, work); len(names) != 0 {
		t.Fatalf("view --attachment must not write local files: %v", names)
	}
}

func TestRevisionsListAndDetail(t *testing.T) {
	f := newFakeELab(t)
	env := f.env(t.TempDir(), false)
	f.raw["GET experiments/42/revisions"] = `[{"id":3,"created_at":"2026-11-01 10:00:00","fullname":"Ada","content_type":1},{"id":12,"created_at":"2026-11-02 09:00:00","fullname":""}]`
	f.raw["GET experiments/42/revisions/3"] = `{"id":3,"body":"old body","content_type":1}`
	f.raw["GET experiments/42/revisions/4"] = `{"id":4,"body":null}`
	f.raw["GET experiments/42/revisions/5"] = `{"id":5}`
	f.raw["GET experiments/42/revisions/6"] = `{"id":6,"body":7}`

	code, stdout, stderr := run(env, "revisions", "42")
	if code != cli.ExitOK || stdout != "3   2026-11-01 10:00:00  Ada\n12  2026-11-02 09:00:00\n" {
		t.Fatalf("list: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, _ = run(env, "revisions", "--json", "42")
	var list []map[string]any
	if code != cli.ExitOK || json.Unmarshal([]byte(stdout), &list) != nil || len(list) != 2 || list[0]["content_type"] != float64(1) {
		t.Fatalf("list --json: code=%d stdout=%q", code, stdout)
	}
	code, stdout, _ = run(env, "revisions", "43")
	if code != cli.ExitOK || stdout != "no revisions\n" {
		t.Fatalf("empty list: code=%d stdout=%q", code, stdout)
	}
	f.raw["GET experiments/44/revisions"] = `null`
	code, stdout, _ = run(env, "revisions", "--json", "44")
	if code != cli.ExitOK || stdout != "[]\n" {
		t.Fatalf("empty --json must print []: code=%d stdout=%q", code, stdout)
	}

	code, stdout, _ = run(env, "revisions", "42", "--rev", "3")
	if code != cli.ExitOK || stdout != "old body" {
		t.Fatalf("detail must print the body verbatim: code=%d stdout=%q", code, stdout)
	}
	code, stdout, _ = run(env, "revisions", "42", "--rev", "4")
	if code != cli.ExitOK || stdout != "" {
		t.Fatalf("null body: code=%d stdout=%q", code, stdout)
	}
	code, stdout, _ = run(env, "revisions", "42", "--rev", "3", "--json")
	if code != cli.ExitOK || !strings.Contains(stdout, `"body": "old body"`) || !strings.Contains(stdout, `"content_type": 1`) {
		t.Fatalf("detail --json: code=%d stdout=%q", code, stdout)
	}
	for rev, want := range map[string]string{"5": "revision response missing body", "6": "revision response has non-string body"} {
		code, _, stderr := run(env, "revisions", "42", "--rev", rev)
		if code != cli.ExitError || !strings.Contains(stderr, want) {
			t.Errorf("--rev %s: code=%d stderr=%q", rev, code, stderr)
		}
	}

	if w := f.writes(); len(w) != 0 {
		t.Fatalf("revisions must never write: %v", w)
	}
	f.reset()
	for _, args := range [][]string{
		{"revisions", "--rev=0", "42"},
		{"revisions", "--rev=-1", "42"},
		{"revisions", "--rev=x", "42"},
		{"revisions", "42", "43"},
	} {
		if code, _, _ := run(env, args...); code != cli.ExitUsage {
			t.Errorf("%v: exit = %d, want %d", args, code, cli.ExitUsage)
		}
	}
	if names := dirNames(t, env.WorkDir); len(names) != 0 {
		t.Fatalf("revisions must not write local files: %v", names)
	}
	if got := f.reset(); len(got) != 0 {
		t.Fatalf("usage errors must not reach the server: %v", got)
	}
}

func TestRevisionsTargets(t *testing.T) {
	f := newFakeELab(t)
	work := t.TempDir()
	env := f.env(work, false)
	f.raw["GET items/8/revisions"] = `[{"id":1,"created_at":"c","fullname":"F"}]`
	f.raw["GET experiments/8/revisions"] = `[{"id":2,"created_at":"c","fullname":"E"}]`

	code, stdout, stderr := run(env, "revisions", f.srv.URL+"/database.php?mode=view&id=8")
	if code != cli.ExitOK || stdout != "1  c  F\n" {
		t.Fatalf("URL target: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if code, _, stderr := run(env, "revisions", "--entity", "experiments", f.srv.URL+"/database.php?mode=view&id=8"); code != cli.ExitError || !strings.Contains(stderr, "entity mismatch") {
		t.Fatalf("URL/--entity conflict: code=%d stderr=%q", code, stderr)
	}
	if code, stdout, _ := run(env, "revisions", "--entity", "items", "8"); code != cli.ExitOK || stdout != "1  c  F\n" {
		t.Fatalf("id with --entity: code=%d stdout=%q", code, stdout)
	}

	write := func(name, content string) string {
		p := filepath.Join(work, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	doc := write("item.md", "---\nid: 8\nentity: items\nprofile: lab\n---\n\nbody\n")
	if code, stdout, stderr := run(env, "revisions", doc); code != cli.ExitOK || stdout != "1  c  F\n" {
		t.Fatalf("local document: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if code, _, stderr := run(env, "revisions", "--entity", "experiments", doc); code != cli.ExitError || !strings.Contains(stderr, "entity mismatch") {
		t.Fatalf("front matter/--entity conflict: code=%d stderr=%q", code, stderr)
	}
	bare := write("bare.md", "---\nid: 8\nprofile: lab\n---\n\nbody\n")
	if code, stdout, stderr := run(env, "revisions", "--entity", "items", bare); code != cli.ExitOK || stdout != "1  c  F\n" {
		t.Fatalf("--entity must apply when front matter has none: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if code, _, stderr := run(env, "revisions", "--profile", "other", doc); code != cli.ExitError || !strings.Contains(stderr, "profile mismatch") {
		t.Fatalf("front matter profile must be used: code=%d stderr=%q", code, stderr)
	}
	noID := write("new.md", "---\ntitle: x\n---\n\nbody\n")
	if code, _, stderr := run(env, "revisions", noID); code != cli.ExitError || !strings.Contains(stderr, "id is required") {
		t.Fatalf("missing id: code=%d stderr=%q", code, stderr)
	}
	if w := f.writes(); len(w) != 0 {
		t.Fatalf("revisions must never write: %v", w)
	}
	if names := dirNames(t, work); len(names) != 3 {
		t.Fatalf("revisions must not add local files: %v", names)
	}
}
