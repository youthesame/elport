package difftest

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

type cliRunner struct {
	name    string
	exe     string
	prefix  []string
	repoDir string
}

type runResult struct {
	code   int
	stdout string
	stderr string
}

func (r cliRunner) run(t *testing.T, workDir, homeDir, baseURL, caFile string, args ...string) runResult {
	t.Helper()
	cmdArgs := append(append([]string(nil), r.prefix...), args...)
	cmd := exec.Command(r.exe, cmdArgs...)
	cmd.Dir = workDir
	env := filteredEnv(os.Environ(), []string{
		"HOME", "USERPROFILE", "ELABFTW_BASE_URL", "ELABFTW_API_KEY",
		"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "NO_COLOR", "COLUMNS",
	})
	env = append(env,
		"HOME="+homeDir,
		"USERPROFILE="+homeDir,
		"NO_COLOR=1",
		"COLUMNS=100",
	)
	if baseURL != "" {
		env = append(env, "ELABFTW_BASE_URL="+baseURL, "ELABFTW_API_KEY=1-diff-secret-key")
	}
	if caFile != "" {
		env = append(env, "SSL_CERT_FILE="+caFile, "REQUESTS_CA_BUNDLE="+caFile)
	}
	cmd.Env = env

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("%s exec %v failed: %v", r.name, args, err)
		}
	}
	return runResult{code: code, stdout: outBuf.String(), stderr: errBuf.String()}
}

func filteredEnv(env []string, drop []string) []string {
	dropSet := make(map[string]bool, len(drop))
	for _, k := range drop {
		dropSet[strings.ToUpper(k)] = true
	}
	out := make([]string, 0, len(env))
	for _, entry := range env {
		k, _, _ := strings.Cut(entry, "=")
		if !dropSet[strings.ToUpper(k)] {
			out = append(out, entry)
		}
	}
	return out
}

func buildRunners(t *testing.T) (cliRunner, cliRunner, bool) {
	t.Helper()
	repoDir, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo dir: %v", err)
	}
	binName := "elport"
	if runtime.GOOS == "windows" {
		binName = "elport.exe"
	}
	goBin := filepath.Join(t.TempDir(), binName)
	buildCmd := exec.Command("go", "build", "-o", goBin, "./cmd/elport")
	buildCmd.Dir = repoDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./cmd/elport failed: %v\n%s", err, out)
	}
	goRunner := cliRunner{name: "go", exe: goBin, repoDir: repoDir}

	requirePython := os.Getenv("ELPORT_DIFF_REQUIRE") == "1"
	uvPath, err := exec.LookPath("uv")
	if err != nil {
		if requirePython {
			t.Fatalf("ELPORT_DIFF_REQUIRE=1 but uv not found in PATH")
		}
		return goRunner, cliRunner{}, false
	}
	probe := exec.Command(uvPath, "run", "--project", repoDir, "python", "-c", "import elport")
	probe.Dir = repoDir
	if err := probe.Run(); err != nil {
		if requirePython {
			t.Fatalf("ELPORT_DIFF_REQUIRE=1 but uv run python -c 'import elport' failed: %v", err)
		}
		return goRunner, cliRunner{}, false
	}
	pyRunner := cliRunner{
		name:    "python",
		exe:     uvPath,
		prefix:  []string{"run", "--project", repoDir, "elport"},
		repoDir: repoDir,
	}
	return goRunner, pyRunner, true
}

type envPair struct {
	home    string
	work    string
	srv     *fakeElabServer
	runner  cliRunner
	baseURL string
}

func newEnvPair(t *testing.T, r cliRunner) *envPair {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	srv, err := newFakeElabServer(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return &envPair{home: home, work: work, srv: srv, runner: r, baseURL: srv.URL()}
}

func (e *envPair) run(t *testing.T, args ...string) runResult {
	t.Helper()
	return e.runner.run(t, e.work, e.home, e.baseURL, e.srv.CAFile(), args...)
}

func (e *envPair) runNoCreds(t *testing.T, args ...string) runResult {
	t.Helper()
	return e.runner.run(t, e.work, e.home, "", "", args...)
}

func snapshotDirFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		out[filepath.ToSlash(rel)] = string(raw)
		return nil
	})
	return out
}

func snapshotState(t *testing.T, homeDir, workDir, baseURL string) map[string]any {
	t.Helper()
	stateDir := filepath.Join(homeDir, ".config", "elport", "state")
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}
		}
		t.Fatalf("read state dir: %v", err)
	}
	resolvedWork, _ := filepath.EvalSymlinks(workDir)
	if resolvedWork == "" {
		resolvedWork = workDir
	}
	out := map[string]any{}
	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(stateDir, de.Name()))
		if err != nil {
			t.Fatalf("read state %s: %v", de.Name(), err)
		}
		// Normalize per-run temp base URL and temp workDir so both runs are directly comparable.
		text := strings.ReplaceAll(string(raw), baseURL, "https://SERVER")
		var obj map[string]any
		if err := json.Unmarshal([]byte(text), &obj); err != nil {
			t.Fatalf("parse state json: %v", err)
		}
		if p, ok := obj["path"].(string); ok {
			pNorm := p
			if rp, err := filepath.EvalSymlinks(p); err == nil {
				pNorm = rp
			}
			rel, err := filepath.Rel(resolvedWork, pNorm)
			if err == nil {
				obj["path"] = "WORK/" + filepath.ToSlash(rel)
			}
		}
		// State filename is sha256(baseURL|entity|id), which varies with server port;
		// key comparison by logical path+body structure instead of port-dependent hash.
		logicalKey := de.Name()
		if p, ok := obj["path"].(string); ok {
			logicalKey = p
		}
		out[logicalKey] = obj
	}
	return out
}

func normalizeRequests(reqs []recordedReq, baseURL string) []recordedReq {
	out := make([]recordedReq, len(reqs))
	for i, r := range reqs {
		cp := r
		if cp.Body != nil {
			raw, _ := json.Marshal(cp.Body)
			norm := strings.ReplaceAll(string(raw), baseURL, "https://SERVER")
			var m map[string]any
			_ = json.Unmarshal([]byte(norm), &m)
			cp.Body = m
		}
		out[i] = cp
	}
	return out
}

func assertEqualDiff(t *testing.T, label string, pyVal, goVal any) {
	t.Helper()
	if !reflect.DeepEqual(pyVal, goVal) {
		pyJSON, _ := json.MarshalIndent(pyVal, "", "  ")
		goJSON, _ := json.MarshalIndent(goVal, "", "  ")
		t.Fatalf("%s mismatch:\n--- python ---\n%s\n--- go ---\n%s", label, pyJSON, goJSON)
	}
}

func TestDifferentialNewPushReuseConflictMergePullFetchAndSafety(t *testing.T) {
	goRunner, pyRunner, hasPython := buildRunners(t)
	if !hasPython {
		t.Skip("uv/python environment not available; set ELPORT_DIFF_REQUIRE=1 to enforce")
	}

	t.Run("new_push_upload_reuse_conflict_merge", func(t *testing.T) {
		py := newEnvPair(t, pyRunner)
		g := newEnvPair(t, goRunner)

		for _, env := range []*envPair{py, g} {
			if err := os.WriteFile(filepath.Join(env.work, "fig.png"), []byte("png-data-123"), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		// 1. elport new
		rPy := py.run(t, "new", "Assay One", "-o", "report.md")
		rGo := g.run(t, "new", "Assay One", "-o", "report.md")
		assertEqualDiff(t, "new exit code", rPy.code, rGo.code)
		if rGo.code != 0 {
			t.Fatalf("new failed: go=%q py=%q", rGo.stderr, rPy.stderr)
		}

		// 2. Edit report.md with local reference + metadata + tag, then push.
		for _, env := range []*envPair{py, g} {
			content := "---\ntitle: Assay One\nentity: experiments\nprofile: default\nid: 100\ncategory: Assay\nstatus: Running\ntags:\n- alpha\n---\n\nline1\n![fig](fig.png)\nline3\n"
			if err := os.WriteFile(filepath.Join(env.work, "report.md"), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		rPy = py.run(t, "push")
		rGo = g.run(t, "push")
		assertEqualDiff(t, "push1 exit code", rPy.code, rGo.code)
		assertEqualDiff(t, "push1 work files", snapshotDirFiles(t, py.work), snapshotDirFiles(t, g.work))
		assertEqualDiff(t, "push1 state", snapshotState(t, py.home, py.work, py.baseURL), snapshotState(t, g.home, g.work, g.baseURL))
		assertEqualDiff(t, "push1 HTTP requests", normalizeRequests(py.srv.Requests(), py.baseURL), normalizeRequests(g.srv.Requests(), g.baseURL))

		// 3. Second push must reuse upload by sha256 and skip PATCH.
		py.srv.ResetLog()
		g.srv.ResetLog()
		rPy = py.run(t, "push")
		rGo = g.run(t, "push")
		assertEqualDiff(t, "push2 exit code", rPy.code, rGo.code)
		assertEqualDiff(t, "push2 HTTP requests", normalizeRequests(py.srv.Requests(), py.baseURL), normalizeRequests(g.srv.Requests(), g.baseURL))

		// 4. Web edit + local edit -> push conflict -> git merge -> push.
		for _, env := range []*envPair{py, g} {
			env.srv.SetRemoteBody("experiments", 100, "line1\n![fig]("+env.baseURL+"/app/download.php?f=ab/500-fig.png&storage=1)\nremote line3\n")
			content := "---\ntitle: Assay One\nentity: experiments\nprofile: default\nid: 100\ncategory: Assay\nstatus: Running\ntags:\n- alpha\n---\n\nlocal line1\n![fig](fig.png)\nline3\n"
			if err := os.WriteFile(filepath.Join(env.work, "report.md"), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		rPy = py.run(t, "push")
		rGo = g.run(t, "push")
		assertEqualDiff(t, "conflict push exit code", rPy.code, rGo.code)
		if rGo.code != 1 {
			t.Fatalf("expected conflict exit 1, got %d", rGo.code)
		}
		assertEqualDiff(t, "conflict sidecars", snapshotDirFiles(t, py.work), snapshotDirFiles(t, g.work))
		assertEqualDiff(t, "conflict state", snapshotState(t, py.home, py.work, py.baseURL), snapshotState(t, g.home, g.work, g.baseURL))

		rPy = py.run(t, "merge")
		rGo = g.run(t, "merge")
		assertEqualDiff(t, "merge exit code", rPy.code, rGo.code)
		assertEqualDiff(t, "post-merge files", snapshotDirFiles(t, py.work), snapshotDirFiles(t, g.work))
		assertEqualDiff(t, "post-merge state", snapshotState(t, py.home, py.work, py.baseURL), snapshotState(t, g.home, g.work, g.baseURL))

		py.srv.ResetLog()
		g.srv.ResetLog()
		rPy = py.run(t, "push")
		rGo = g.run(t, "push")
		assertEqualDiff(t, "post-merge push exit code", rPy.code, rGo.code)
		assertEqualDiff(t, "post-merge push HTTP requests", normalizeRequests(py.srv.Requests(), py.baseURL), normalizeRequests(g.srv.Requests(), g.baseURL))
	})

	t.Run("lock_html_remote_linked_and_doc_links", func(t *testing.T) {
		py := newEnvPair(t, pyRunner)
		g := newEnvPair(t, goRunner)

		for _, env := range []*envPair{py, g} {
			env.srv.SeedEntity("experiments", 10, map[string]any{"title": "Locked", "body": "old\n", "locked": 1})
			env.srv.SeedEntity("experiments", 11, map[string]any{"title": "HTML", "body": "<p>Rich</p>", "content_type": 1})
			env.srv.SeedEntity("experiments", 12, map[string]any{"title": "RemoteLink", "body": "[s](secret.csv)\n"})
			env.srv.SeedEntity("items", 20, map[string]any{"title": "Buffer A", "body": "buf\n"})
			if err := os.WriteFile(filepath.Join(env.work, "secret.csv"), []byte("top-secret"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(env.work, "locked.md"), []byte("---\nid: 10\nentity: experiments\nprofile: default\n---\n\nnew\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(env.work, "html.md"), []byte("---\nid: 11\nentity: experiments\nprofile: default\n---\n\nnew\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(env.work, "item20.md"), []byte("---\nid: 20\nentity: items\nprofile: default\n---\n\nbuf\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(env.work, "rl.md"), []byte("---\nid: 12\nentity: experiments\nprofile: default\n---\n\n[s](secret.csv) [b](item20.md)\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		// Locked refused even with --force.
		assertEqualDiff(t, "locked push --force code", py.run(t, "push", "locked.md", "--force").code, g.run(t, "push", "locked.md", "--force").code)
		// HTML refused by push --force, pull, clone.
		assertEqualDiff(t, "html push --force code", py.run(t, "push", "html.md", "--force").code, g.run(t, "push", "html.md", "--force").code)
		assertEqualDiff(t, "html pull code", py.run(t, "pull", "html.md").code, g.run(t, "pull", "html.md").code)
		assertEqualDiff(t, "html clone code", py.run(t, "clone", "11", "-o", "cloned_html.md").code, g.run(t, "clone", "11", "-o", "cloned_html.md").code)
		// HTML readable by view -o DIR.
		assertEqualDiff(t, "html view -o code", py.run(t, "view", "11", "-o", "snap11").code, g.run(t, "view", "11", "-o", "snap11").code)

		// Remote-linked secret.csv NOT uploaded, while item20.md IS linked via database.php URL + POST /items_links/20.
		py.srv.ResetLog()
		g.srv.ResetLog()
		rPy := py.run(t, "push", "rl.md", "--force")
		rGo := g.run(t, "push", "rl.md", "--force")
		assertEqualDiff(t, "remote-link + doc-link push code", rPy.code, rGo.code)
		assertEqualDiff(t, "remote-link + doc-link HTTP requests", normalizeRequests(py.srv.Requests(), py.baseURL), normalizeRequests(g.srv.Requests(), g.baseURL))
		assertEqualDiff(t, "work files", snapshotDirFiles(t, py.work), snapshotDirFiles(t, g.work))
	})

	t.Run("clone_all_and_comments_and_fetch", func(t *testing.T) {
		py := newEnvPair(t, pyRunner)
		g := newEnvPair(t, goRunner)

		for _, env := range []*envPair{py, g} {
			env.srv.SeedEntity("experiments", 30, map[string]any{
				"title":          "Clone MD",
				"body":           "body 30\n",
				"category_title": "Assay",
				"tags":           "t1|t2",
			})
			env.srv.SeedUpload("experiments", 30, "extra.bin", []byte("extra-bytes"))
			env.srv.SeedEntity("experiments", 31, map[string]any{
				"title":        "Clone HTML",
				"body":         "<p>HTML 31</p>",
				"content_type": 1,
			})
		}

		rPy := py.run(t, "clone", "--all", "backup")
		rGo := g.run(t, "clone", "--all", "backup")
		assertEqualDiff(t, "clone --all exit code", rPy.code, rGo.code)
		assertEqualDiff(t, "clone --all files", snapshotDirFiles(t, py.work), snapshotDirFiles(t, g.work))
		assertEqualDiff(t, "clone --all state", snapshotState(t, py.home, py.work, py.baseURL), snapshotState(t, g.home, g.work, g.baseURL))

		// fetch unreferenced attachment into cloned MD doc dir.
		mdDoc := filepath.Join("backup", "experiments", "30-Clone MD", "report.md")
		rPy = py.run(t, "fetch", mdDoc)
		rGo = g.run(t, "fetch", mdDoc)
		assertEqualDiff(t, "fetch exit code", rPy.code, rGo.code)
		assertEqualDiff(t, "post-fetch files", snapshotDirFiles(t, py.work), snapshotDirFiles(t, g.work))

		// comment + comments by URL.
		rPy = py.run(t, "comment", py.baseURL+"/experiments.php?mode=view&id=30", "checked calibration")
		rGo = g.run(t, "comment", g.baseURL+"/experiments.php?mode=view&id=30", "checked calibration")
		assertEqualDiff(t, "comment exit code", rPy.code, rGo.code)
		cPy := py.run(t, "comments", "30")
		cGo := g.run(t, "comments", "30")
		assertEqualDiff(t, "comments exit code", cPy.code, cGo.code)
		assertEqualDiff(t, "comments stdout", cPy.stdout, cGo.stdout)
	})

	t.Run("pull_metadata_merge_dry_run_and_browse", func(t *testing.T) {
		py := newEnvPair(t, pyRunner)
		g := newEnvPair(t, goRunner)

		for _, env := range []*envPair{py, g} {
			env.srv.SeedEntity("experiments", 40, map[string]any{
				"title":          "Original Title",
				"body":           "base body\n",
				"category_title": "Assay",
				"status_title":   "Running",
				"tags":           "t1",
			})
			env.srv.SeedUpload("experiments", 40, "raw.csv", []byte("remote-csv"))
		}

		// Clone, then test pull metadata-only when local is dirty & remote body unchanged.
		assertEqualDiff(t, "clone 40 code", py.run(t, "clone", "40", "-o", "report.md").code, g.run(t, "clone", "40", "-o", "report.md").code)
		for _, env := range []*envPair{py, g} {
			env.srv.SetRemoteField("experiments", 40, "category_title", "Protocol")
			env.srv.SetRemoteField("experiments", 40, "tags", "t1|webtag")
			doc := "---\nid: 40\nentity: experiments\nprofile: default\ntitle: Local Title\ncategory: Assay\nstatus: Running\nread: team\nwrite: team\ntags:\n- t1\n- localtag\n---\n\nlocal dirty body\n"
			if err := os.WriteFile(filepath.Join(env.work, "report.md"), []byte(doc), 0o644); err != nil {
				t.Fatal(err)
			}
			// Create a conflicting local raw.csv so fetch writes raw.csv.remote.
			if err := os.WriteFile(filepath.Join(env.work, "raw.csv"), []byte("local-csv"), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		assertEqualDiff(t, "meta-only pull code", py.run(t, "pull").code, g.run(t, "pull").code)
		assertEqualDiff(t, "meta-only pull files", snapshotDirFiles(t, py.work), snapshotDirFiles(t, g.work))
		assertEqualDiff(t, "meta-only pull state", snapshotState(t, py.home, py.work, py.baseURL), snapshotState(t, g.home, g.work, g.baseURL))

		// fetch writes raw.csv.remote conflict copy without overwriting raw.csv.
		assertEqualDiff(t, "fetch conflict code", py.run(t, "fetch").code, g.run(t, "fetch").code)
		assertEqualDiff(t, "fetch conflict files", snapshotDirFiles(t, py.work), snapshotDirFiles(t, g.work))

		// push --dry-run does not mutate remote.
		py.srv.ResetLog()
		g.srv.ResetLog()
		dPy := py.run(t, "push", "-n")
		dGo := g.run(t, "push", "-n")
		assertEqualDiff(t, "dry-run exit code", dPy.code, dGo.code)
		assertEqualDiff(t, "dry-run stdout", dPy.stdout, dGo.stdout)
		assertEqualDiff(t, "dry-run HTTP requests", normalizeRequests(py.srv.Requests(), py.baseURL), normalizeRequests(g.srv.Requests(), g.baseURL))

		// whoami & list --json parity.
		wPy := py.run(t, "whoami")
		wGo := g.run(t, "whoami")
		assertEqualDiff(t, "whoami exit code", wPy.code, wGo.code)
		assertEqualDiff(t, "whoami stdout", strings.ReplaceAll(wPy.stdout, py.baseURL[len("https://"):], "HOST"), strings.ReplaceAll(wGo.stdout, g.baseURL[len("https://"):], "HOST"))

		lPy := py.run(t, "list", "--json")
		lGo := g.run(t, "list", "--json")
		assertEqualDiff(t, "list --json exit code", lPy.code, lGo.code)
		var pyRows, goRows any
		_ = json.Unmarshal([]byte(strings.ReplaceAll(lPy.stdout, py.baseURL, "https://SERVER")), &pyRows)
		_ = json.Unmarshal([]byte(strings.ReplaceAll(lGo.stdout, g.baseURL, "https://SERVER")), &goRows)
		assertEqualDiff(t, "list --json rows", pyRows, goRows)
	})
}

func TestStateRoundTripBetweenPythonAndGo(t *testing.T) {
	goRunner, pyRunner, hasPython := buildRunners(t)
	if !hasPython {
		t.Skip("uv/python environment not available; set ELPORT_DIFF_REQUIRE=1 to enforce")
	}

	root := t.TempDir()
	home := filepath.Join(root, "home")
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	srv, err := newFakeElabServer(root)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	// Configure base_url only (no api_key) so diff --base works without credentials.
	cfgDir := filepath.Join(home, ".config", "elport")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte("default_profile = \"default\"\n\n[profiles.default]\nbase_url = \""+srv.URL()+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	runPy := func(args ...string) runResult {
		return pyRunner.run(t, work, home, srv.URL(), srv.CAFile(), args...)
	}
	runGo := func(args ...string) runResult {
		return goRunner.run(t, work, home, srv.URL(), srv.CAFile(), args...)
	}

	if err := os.WriteFile(filepath.Join(work, "data.csv"), []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. Python creates and pushes an entity with an attachment.
	if res := runPy("new", "Roundtrip", "-o", "report.md"); res.code != 0 {
		t.Fatalf("python new failed: %s", res.stderr)
	}
	docBody := "---\ntitle: Roundtrip\nentity: experiments\nprofile: default\nid: 100\n---\n\nfirst\n[data](data.csv)\nlast\n"
	if err := os.WriteFile(filepath.Join(work, "report.md"), []byte(docBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := runPy("push"); res.code != 0 {
		t.Fatalf("python push failed: %s", res.stderr)
	}

	// 2. Go reads Python's state: status must report clean/unchanged, diff --base empty without creds.
	if res := runGo("status"); res.code != 0 || !strings.Contains(res.stdout, "local: clean") || !strings.Contains(res.stdout, "remote: unchanged") {
		t.Fatalf("go status on python state failed: code=%d out=%q err=%q", res.code, res.stdout, res.stderr)
	}
	if res := goRunner.run(t, work, home, "", "", "diff", "--base"); res.code != 0 || res.stdout != "" {
		t.Fatalf("go diff --base without creds failed: code=%d out=%q err=%q", res.code, res.stdout, res.stderr)
	}

	// 3. Diverge remote + local -> Python hits conflict -> Go merges & pushes -> Python sees clean up to date!
	srv.SetRemoteBody("experiments", 100, "first\n[data]("+srv.URL()+"/app/download.php?f=ab/500-data.csv&storage=1)\nremote last\n")
	localEdit := "---\ntitle: Roundtrip\nentity: experiments\nprofile: default\nid: 100\n---\n\nlocal first\n[data](data.csv)\nlast\n"
	if err := os.WriteFile(filepath.Join(work, "report.md"), []byte(localEdit), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := runPy("pull"); res.code != 1 {
		t.Fatalf("expected python pull conflict, got code=%d", res.code)
	}
	if res := runGo("merge"); res.code != 0 {
		t.Fatalf("go merge on python conflict state failed: %s", res.stderr)
	}
	if res := runGo("push"); res.code != 0 {
		t.Fatalf("go push after merge failed: %s", res.stderr)
	}

	// 4. Python reads Go's updated state: status must report clean/unchanged and reuse data.csv!
	resPy := runPy("status")
	if resPy.code != 0 || !strings.Contains(resPy.stdout, "local: clean") || !strings.Contains(resPy.stdout, "remote: unchanged") || !strings.Contains(resPy.stdout, "uploads reuse: data.csv") {
		t.Fatalf("python status on go state failed: code=%d out=%q err=%q", resPy.code, resPy.stdout, resPy.stderr)
	}

	// Verify there is exactly 1 state file (identical sha256 state key hash between Python and Go!).
	entries, err := os.ReadDir(filepath.Join(home, ".config", "elport", "state"))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if len(names) != 1 {
		t.Fatalf("expected 1 shared state file between Python and Go, got %v", names)
	}
}
