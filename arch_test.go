package elport_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const modulePrefix = "github.com/youthesame/elport/"

var bannedDocRefPattern = regexp.MustCompile(`\bADR-\d+\b|DESIGN\.md`)

var forbiddenImportsByPkg = map[string][]string{
	"internal/frontmatter": {
		"net/http", "os/exec",
		modulePrefix + "internal/config",
		modulePrefix + "internal/client",
		modulePrefix + "internal/state",
		modulePrefix + "internal/sync",
		modulePrefix + "internal/plan",
		modulePrefix + "internal/browse",
		modulePrefix + "internal/cli",
	},
	"internal/transclude": {
		"regexp", "net/http", "os/exec",
		modulePrefix + "internal/config",
		modulePrefix + "internal/client",
		modulePrefix + "internal/state",
		modulePrefix + "internal/sync",
		modulePrefix + "internal/plan",
		modulePrefix + "internal/browse",
		modulePrefix + "internal/cli",
	},
	"internal/config": {
		"net/http", "os/exec",
		modulePrefix + "internal/client",
		modulePrefix + "internal/sync",
		modulePrefix + "internal/plan",
		modulePrefix + "internal/transclude",
		modulePrefix + "internal/browse",
		modulePrefix + "internal/cli",
	},
	"internal/state": {
		"net/http", "os/exec",
		modulePrefix + "internal/client",
		modulePrefix + "internal/sync",
		modulePrefix + "internal/plan",
		modulePrefix + "internal/transclude",
		modulePrefix + "internal/browse",
		modulePrefix + "internal/cli",
	},
	"internal/browse": {
		"os/exec",
		modulePrefix + "internal/state",
		modulePrefix + "internal/sync",
		modulePrefix + "internal/plan",
		modulePrefix + "internal/cli",
		modulePrefix + "internal/fsx",
	},
	"internal/plan": {
		"net/http", "os/exec",
		modulePrefix + "internal/client",
		modulePrefix + "internal/sync",
		modulePrefix + "internal/browse",
		modulePrefix + "internal/cli",
		modulePrefix + "internal/render",
	},
	"internal/render": {
		"os", "net/http", "os/exec",
		modulePrefix + "internal/config",
		modulePrefix + "internal/client",
		modulePrefix + "internal/state",
		modulePrefix + "internal/sync",
		modulePrefix + "internal/plan",
		modulePrefix + "internal/browse",
		modulePrefix + "internal/cli",
		modulePrefix + "internal/fsx",
	},
}

func allowedProdComment(text string) bool {
	for _, prefix := range []string{"//go:", "//line ", "//export ", "// Deprecated:"} {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

func pkgMatches(pkgDir, root string) bool {
	return pkgDir == root || strings.HasPrefix(pkgDir, root+"/")
}

func importMatches(importPath, forbidden string) bool {
	return importPath == forbidden || strings.HasPrefix(importPath, forbidden+"/")
}

func importAliases(f *ast.File) map[string]string {
	aliases := map[string]string{}
	for _, spec := range f.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		var name string
		if spec.Name != nil {
			name = spec.Name.Name
		} else {
			parts := strings.Split(importPath, "/")
			name = parts[len(parts)-1]
		}
		if name != "_" && name != "." {
			aliases[name] = importPath
		}
	}
	return aliases
}

func checkGoSource(relPath string, src any) []string {
	rel := filepath.ToSlash(relPath)
	isTest := strings.HasSuffix(rel, "_test.go")
	pkgDir := filepath.ToSlash(filepath.Dir(rel))

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, src, parser.ParseComments)
	if err != nil {
		return []string{fmt.Sprintf("%s: parse error: %v", rel, err)}
	}

	var violations []string
	add := func(pos token.Pos, format string, args ...any) {
		p := fset.Position(pos)
		violations = append(violations, fmt.Sprintf("%s:%d: %s", rel, p.Line, fmt.Sprintf(format, args...)))
	}

	// Comments are banned in production code (only compiler/tool directives allowed)
	// and historical ADR/DESIGN citations are banned in all Go files so tests state
	// their rationale directly instead of pointing to retired docs.
	for _, group := range file.Comments {
		for _, c := range group.List {
			if !isTest && !allowedProdComment(c.Text) {
				add(c.Pos(), "comments are forbidden in production code; move rationale to tests or encode in types")
			}
			if rel != "arch_test.go" && bannedDocRefPattern.MatchString(c.Text) {
				add(c.Pos(), "do not cite ADR numbers or DESIGN.md in Go files")
			}
		}
	}

	if isTest {
		if rel != "arch_test.go" {
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if ok && lit.Kind == token.STRING {
					val, err := strconv.Unquote(lit.Value)
					if err == nil && bannedDocRefPattern.MatchString(val) {
						add(lit.Pos(), "do not cite ADR numbers or DESIGN.md in Go files")
					}
				}
				return true
			})
		}
		return violations
	}

	var forbiddenImports []string
	for rulePkg, imps := range forbiddenImportsByPkg {
		if pkgMatches(pkgDir, rulePkg) {
			forbiddenImports = append(forbiddenImports, imps...)
		}
	}

	importsHTTP := false
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		if spec.Name != nil && spec.Name.Name == "." {
			add(spec.Pos(), "dot imports are forbidden in production code")
		}
		if importPath == "net/http" {
			importsHTTP = true
		}
		for _, forbidden := range forbiddenImports {
			if importMatches(importPath, forbidden) {
				add(spec.Pos(), "package %s must not import %q", pkgDir, importPath)
			}
		}
		if importPath == "os/exec" && !pkgMatches(pkgDir, "internal/sync") {
			add(spec.Pos(), "os/exec is allowed only in internal/sync")
		}
	}

	aliases := importAliases(file)

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BasicLit:
			if node.Kind == token.STRING {
				val, err := strconv.Unquote(node.Value)
				if err == nil {
					if bannedDocRefPattern.MatchString(val) {
						add(node.Pos(), "do not cite ADR numbers or DESIGN.md in Go files")
					}
					if val == "XDG_CONFIG_HOME" {
						add(node.Pos(), "do not reference XDG_CONFIG_HOME; always use <home>/.config/elport")
					}
					if pkgMatches(pkgDir, "internal/client") && val == "DELETE" {
						add(node.Pos(), "HTTP DELETE is forbidden in internal/client")
					}
				}
			}
		case *ast.SelectorExpr:
			ident, ok := node.X.(*ast.Ident)
			if !ok {
				return true
			}
			pkgPath := aliases[ident.Name]
			sel := node.Sel.Name
			if pkgPath == "os" && sel == "UserConfigDir" {
				add(node.Pos(), "os.UserConfigDir is forbidden; always use <home>/.config/elport")
			}
			if pkgPath == "os" && sel == "Exit" && rel != "cmd/elport/main.go" {
				add(node.Pos(), "os.Exit is allowed only in cmd/elport/main.go")
			}
			if pkgPath == "net/http" {
				switch sel {
				case "DefaultClient", "Get", "Post", "PostForm", "Head":
					add(node.Pos(), "net/http.%s is forbidden; use the configured client constructor", sel)
				case "MethodDelete":
					add(node.Pos(), "http.MethodDelete is forbidden")
				}
			}
		case *ast.CompositeLit:
			sel, ok := node.Type.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok || aliases[ident.Name] != "net/http" || sel.Sel.Name != "Client" {
				return true
			}
			for _, elt := range node.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Timeout" {
					add(kv.Pos(), "http.Client.Timeout is forbidden; use split dial/header/idle-read timeouts")
				}
			}
		case *ast.AssignStmt:
			if !importsHTTP {
				return true
			}
			for _, lhs := range node.Lhs {
				if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == "Timeout" {
					add(sel.Pos(), "assigning Timeout in net/http code is forbidden; use split dial/header/idle-read timeouts")
				}
			}
		}
		return true
	})

	return violations
}

func TestArchitectureInvariants(t *testing.T) {
	skipTopDirs := map[string]bool{
		".git":     true,
		".venv":    true,
		"dist":     true,
		"testdata": true,
		"vendor":   true,
	}
	var allViolations []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(path)
		if d.IsDir() && rel != "." {
			if skipTopDirs[rel] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") {
			return nil
		}
		allViolations = append(allViolations, checkGoSource(rel, nil)...)
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
	for _, v := range allViolations {
		t.Error(v)
	}
}

func TestArchRulesCatchViolations(t *testing.T) {
	cases := []struct {
		name      string
		relPath   string
		src       string
		wantMatch string
	}{
		{
			name:      "production comment rejected",
			relPath:   "internal/cli/sample.go",
			src:       "package cli\n// hack workaround\nfunc f() {}\n",
			wantMatch: "comments are forbidden in production code",
		},
		{
			name:      "ADR citation in test rejected",
			relPath:   "internal/sync/sample_test.go",
			src:       "package sync_test\n// per ADR-0002\nfunc f() {}\n",
			wantMatch: "do not cite ADR numbers",
		},
		{
			name:      "escaped DESIGN.md citation in test string rejected",
			relPath:   "internal/sync/sample_test.go",
			src:       "package sync_test\nconst s = \"DESIGN\\x2emd\"\n",
			wantMatch: "do not cite ADR numbers or DESIGN.md",
		},
		{
			name:      "dot import rejected",
			relPath:   "internal/cli/dot.go",
			src:       "package cli\nimport . \"os\"\nfunc f() { Exit(1) }\n",
			wantMatch: "dot imports are forbidden",
		},
		{
			name:      "regexp in transclude rejected",
			relPath:   "internal/transclude/scan.go",
			src:       "package transclude\nimport \"regexp\"\nvar _ = regexp.MustCompile\n",
			wantMatch: `must not import "regexp"`,
		},
		{
			name:      "regexp in transclude subpackage rejected",
			relPath:   "internal/transclude/scan/lexer.go",
			src:       "package scan\nimport \"regexp\"\nvar _ = regexp.MustCompile\n",
			wantMatch: `must not import "regexp"`,
		},
		{
			name:      "os.UserConfigDir via alias rejected",
			relPath:   "internal/config/paths.go",
			src:       "package config\nimport o \"os\"\nvar _ = o.UserConfigDir\n",
			wantMatch: "os.UserConfigDir is forbidden",
		},
		{
			name:      "XDG_CONFIG_HOME literal rejected",
			relPath:   "internal/config/paths.go",
			src:       "package config\nimport \"os\"\nfunc f() { _ = os.Getenv(\"XDG_CONFIG_HOME\") }\n",
			wantMatch: "XDG_CONFIG_HOME",
		},
		{
			name:      "os.Exit outside main rejected",
			relPath:   "internal/cli/exit.go",
			src:       "package cli\nimport \"os\"\nfunc f() { os.Exit(1) }\n",
			wantMatch: "os.Exit is allowed only in cmd/elport/main.go",
		},
		{
			name:      "os/exec outside sync rejected",
			relPath:   "internal/cli/exec.go",
			src:       "package cli\nimport \"os/exec\"\nvar _ = exec.Command\n",
			wantMatch: "os/exec is allowed only in internal/sync",
		},
		{
			name:      "http.MethodDelete rejected",
			relPath:   "internal/client/del.go",
			src:       "package client\nimport \"net/http\"\nvar _ = http.MethodDelete\n",
			wantMatch: "http.MethodDelete is forbidden",
		},
		{
			name:      "DELETE string in client subpackage rejected",
			relPath:   "internal/client/sub/del.go",
			src:       "package sub\nconst m = \"DELETE\"\n",
			wantMatch: "HTTP DELETE is forbidden",
		},
		{
			name:      "http.Client.Timeout composite literal rejected",
			relPath:   "internal/client/c.go",
			src:       "package client\nimport (\n\"net/http\"\n\"time\"\n)\nvar _ = &http.Client{Timeout: time.Second}\n",
			wantMatch: "http.Client.Timeout is forbidden",
		},
		{
			name:      "http.Client.Timeout field assignment rejected",
			relPath:   "internal/client/c.go",
			src:       "package client\nimport (\n\"net/http\"\n\"time\"\n)\nfunc f() { c := &http.Client{}; c.Timeout = time.Second; _ = c }\n",
			wantMatch: "assigning Timeout in net/http code is forbidden",
		},
		{
			name:      "http.DefaultClient rejected",
			relPath:   "internal/client/c.go",
			src:       "package client\nimport \"net/http\"\nvar _ = http.DefaultClient\n",
			wantMatch: "net/http.DefaultClient is forbidden",
		},
		{
			name:      "os import in render rejected",
			relPath:   "internal/render/bad.go",
			src:       "package render\nimport \"os\"\nvar _ = os.ReadFile\n",
			wantMatch: `must not import "os"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := checkGoSource(tc.relPath, tc.src)
			if len(got) == 0 {
				t.Fatalf("expected violation containing %q, got none", tc.wantMatch)
			}
			joined := strings.Join(got, "\n")
			if !strings.Contains(joined, tc.wantMatch) {
				t.Fatalf("violations %q did not contain %q", joined, tc.wantMatch)
			}
		})
	}
}

func TestArchRulesAllowValidCode(t *testing.T) {
	cases := []struct {
		name    string
		relPath string
		src     string
	}{
		{
			name:    "go:embed directive allowed in prod",
			relPath: "internal/help/h.go",
			src:     "package help\nimport _ \"embed\"\n//go:embed agents.md\nvar x string\n",
		},
		{
			name:    "comments allowed in test files",
			relPath: "internal/sync/s_test.go",
			src:     "package sync_test\n// Explain why Windows symlinks need Lstat.\nfunc f() {}\n",
		},
		{
			name:    "os.Exit allowed in cmd/elport/main.go",
			relPath: "cmd/elport/main.go",
			src:     "package main\nimport \"os\"\nfunc main() { os.Exit(0) }\n",
		},
		{
			name:    "os/exec allowed in internal/sync",
			relPath: "internal/sync/merge.go",
			src:     "package sync\nimport \"os/exec\"\nvar _ = exec.Command\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkGoSource(tc.relPath, tc.src); len(got) != 0 {
				t.Fatalf("unexpected violations: %v", got)
			}
		})
	}
}

var agentsSymbolPattern = regexp.MustCompile("`(internal/[a-z0-9_/]+)\\.([A-Z][A-Za-z0-9_]*)(?:\\.([A-Z][A-Za-z0-9_]*))?`")

func exportedSymbolsInPkg(pkgDir string) (map[string]bool, map[string]bool, error) {
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return nil, nil, err
	}
	topLevel := map[string]bool{}
	methods := map[string]bool{}
	fset := token.NewFileSet()
	for _, de := range entries {
		name := de.Name()
		if de.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(pkgDir, name), nil, 0)
		if err != nil {
			return nil, nil, err
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					topLevel[d.Name.Name] = true
				} else if len(d.Recv.List) > 0 {
					recvType := receiverTypeName(d.Recv.List[0].Type)
					if recvType != "" {
						methods[recvType+"."+d.Name.Name] = true
					}
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						topLevel[s.Name.Name] = true
					case *ast.ValueSpec:
						for _, ident := range s.Names {
							topLevel[ident.Name] = true
						}
					}
				}
			}
		}
	}
	return topLevel, methods, nil
}

func receiverTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

func TestArchAgentsSymbolLinksExist(t *testing.T) {
	// Every Go symbol referenced in AGENTS.md must exist in production code so
	// invariant documentation cannot silently drift from the implementation.
	raw, err := os.ReadFile("AGENTS.md")
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	matches := agentsSymbolPattern.FindAllStringSubmatch(string(raw), -1)
	if len(matches) == 0 {
		t.Fatal("AGENTS.md contains no verifiable internal/<pkg>.<Symbol> references")
	}
	type pkgCache struct {
		top     map[string]bool
		methods map[string]bool
	}
	cache := map[string]pkgCache{}
	for _, m := range matches {
		pkgDir, sym, method := m[1], m[2], m[3]
		entry, ok := cache[pkgDir]
		if !ok {
			top, meths, err := exportedSymbolsInPkg(pkgDir)
			if err != nil {
				t.Errorf("AGENTS.md references %s: %v", m[0], err)
				continue
			}
			entry = pkgCache{top: top, methods: meths}
			cache[pkgDir] = entry
		}
		if !entry.top[sym] {
			t.Errorf("AGENTS.md references missing symbol %s in %s", sym, pkgDir)
			continue
		}
		if method != "" && !entry.methods[sym+"."+method] {
			t.Errorf("AGENTS.md references missing method %s.%s in %s", sym, method, pkgDir)
		}
	}
}
