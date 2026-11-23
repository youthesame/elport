package elport_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
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
				if ok && lit.Kind == token.STRING && bannedDocRefPattern.MatchString(lit.Value) {
					add(lit.Pos(), "do not cite ADR numbers or DESIGN.md in Go files")
				}
				return true
			})
		}
		return violations
	}

	forbiddenSet := map[string]bool{}
	for _, imp := range forbiddenImportsByPkg[pkgDir] {
		forbiddenSet[imp] = true
	}

	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		if forbiddenSet[importPath] {
			add(spec.Pos(), "package %s must not import %q", pkgDir, importPath)
		}
		if importPath == "os/exec" && pkgDir != "internal/sync" {
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
					if pkgDir == "internal/client" && val == "DELETE" {
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
		}
		return true
	})

	return violations
}

func TestArchitectureInvariants(t *testing.T) {
	var allViolations []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == ".venv" || name == "dist" || name == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		allViolations = append(allViolations, checkGoSource(path, nil)...)
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
			name:      "DESIGN.md citation rejected",
			relPath:   "internal/sync/sample_test.go",
			src:       "package sync_test\n// see DESIGN.md\nfunc f() {}\n",
			wantMatch: "do not cite ADR numbers or DESIGN.md",
		},
		{
			name:      "regexp in transclude rejected",
			relPath:   "internal/transclude/scan.go",
			src:       "package transclude\nimport \"regexp\"\nvar _ = regexp.MustCompile\n",
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
			name:      "DELETE string in client rejected",
			relPath:   "internal/client/del.go",
			src:       "package client\nconst m = \"DELETE\"\n",
			wantMatch: "HTTP DELETE is forbidden",
		},
		{
			name:      "http.Client.Timeout rejected",
			relPath:   "internal/client/c.go",
			src:       "package client\nimport (\n\"net/http\"\n\"time\"\n)\nvar _ = &http.Client{Timeout: time.Second}\n",
			wantMatch: "http.Client.Timeout is forbidden",
		},
		{
			name:      "http.DefaultClient rejected",
			relPath:   "internal/client/c.go",
			src:       "package client\nimport \"net/http\"\nvar _ = http.DefaultClient\n",
			wantMatch: "net/http.DefaultClient is forbidden",
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
