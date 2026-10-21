// openpgp_guard_test.go enforces the accepted disposition of OSV advisory
// GO-2026-5932 (golang.org/x/crypto/openpgp is unmaintained and unsafe by
// design; NO fixed version exists at any x/crypto release). The advisory is
// module-level noise for this server as long as - and ONLY as long as - the
// openpgp subpackage is never imported. This guard makes that condition
// enforceable instead of a one-time observation: it fails the suite the
// moment any x/crypto/openpgp import appears, directly in this module's
// source or transitively through a dependency.
//
// Deliberately narrow: only the openpgp subtree is guarded. Genuine future
// advisories against other x/crypto packages must keep flagging in scans.
//
// Full write-up for scanner-runners: ops/docs/ACCEPTED_ADVISORIES.md.
package gates

import (
	"bufio"
	"bytes"
	"errors"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const openpgpImportPath = "golang.org/x/crypto/openpgp"

// isOpenpgpImport reports whether path is the openpgp subpackage or
// anything beneath it (openpgp/armor, openpgp/packet, ...).
func isOpenpgpImport(path string) bool {
	return path == openpgpImportPath || strings.HasPrefix(path, openpgpImportPath+"/")
}

// TestNoOpenpgpImportInSource parses the import block of every .go file in
// the module (test files included) and fails on any openpgp import. Reading
// the files here also keys Go's test cache to their contents, so this
// cannot return a stale cached PASS after an import is added anywhere in
// the tree.
func TestNoOpenpgpImportInSource(t *testing.T) {
	// Long tier (testsupport/tier.Long; this package may import nothing internal).
	if testing.Short() {
		t.Skip("long tier (check:server:long, release freeze): no openpgp import in any source file")
	}
	root, rootErr := ModuleRoot()
	if rootErr != nil {
		t.Fatalf("openpgp guard: locate module root: %v", rootErr)
	}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip non-source trees: VCS/state dirs and runtime output.
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "logs" || name == "temp" || name == "node_modules") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		file, parseErr := parser.ParseFile(fset, path, src, parser.ImportsOnly)
		if parseErr != nil {
			// A file another workstream has mid-edit is not this guard's
			// business; the compiler owns syntax.
			t.Logf("openpgp guard: skipping unparseable %s: %v", path, parseErr)
			return nil
		}
		for _, imp := range file.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if isOpenpgpImport(importPath) {
				t.Errorf("%s imports %s: golang.org/x/crypto/openpgp is unmaintained and unsafe by design (GO-2026-5932, no fixed version exists). Remove the import; see ops/docs/ACCEPTED_ADVISORIES.md", path, importPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("openpgp guard walk failed: %v", err)
	}
}

// TestNoOpenpgpImportInBuildClosure asks the go tool for the full package
// closure of the module's build (go list -e -deps ./...) and fails if the
// openpgp subtree appears anywhere in it - this also catches a transitive
// introduction through a third-party dependency, which the source walk
// cannot see.
//
// The -e flag keeps the enumeration alive while some package in the module
// fails to TYPE-check (a concurrent workstream mid-refactor): the import
// graph is fully known for type-broken-but-parseable code, so the closure
// stays complete. Only a PARSE-broken file can hide its own imports from
// the enumeration, and the source-walk layer above logs exactly those files
// as skipped - between the two layers nothing disappears silently.
func TestNoOpenpgpImportInBuildClosure(t *testing.T) {
	// Long tier (testsupport/tier.Long; this package may import nothing internal).
	if testing.Short() {
		t.Skip("long tier (check:server:long, release freeze): no openpgp package in the whole build closure")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("go tool not on PATH: %v", err)
	}
	root, rootErr := ModuleRoot()
	if rootErr != nil {
		t.Fatalf("openpgp guard: locate module root: %v", rootErr)
	}

	// Read go.mod/go.sum so a dependency bump invalidates this test's
	// cached result even though the exec below is invisible to the cache.
	for _, f := range []string{"go.mod", "go.sum"} {
		if _, readErr := os.ReadFile(filepath.Join(root, f)); readErr != nil {
			t.Fatalf("openpgp guard: read %s: %v", f, readErr)
		}
	}

	cmd := exec.Command(goBin, "list", "-e", "-deps", "./...")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if exitErr := (*exec.ExitError)(nil); errors.As(err, &exitErr) {
			stderr = string(exitErr.Stderr)
		}
		t.Fatalf("go list -e -deps ./... failed: %v\n%s", err, stderr)
	}

	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		pkg := strings.TrimSpace(scanner.Text())
		if isOpenpgpImport(pkg) {
			t.Errorf("build closure contains %s: golang.org/x/crypto/openpgp is unmaintained and unsafe by design (GO-2026-5932, no fixed version exists). Find and remove whatever pulls it in; see ops/docs/ACCEPTED_ADVISORIES.md", pkg)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning go list output: %v", err)
	}
}
