// gofmt_gate_test.go enforces the formatting invariant: every .go file in
// this module is byte-identical to its gofmt output, which also pins LF line
// endings. It runs as part of the ordinary `go test ./...` flow and lives in
// the import-free gates package so it can report formatting drift while the
// rest of the module is mid-refactor.
//
// On failure it names every offending file; the fix is always:
//
//	gofmt -w <file>    (or gofmt -w . from the module root)
//
// Unparseable files are logged and skipped, not failed: the compiler owns
// syntax, and a file another workstream has mid-edit is not this gate's
// business. Line-ending policy for non-Go files lives in .gitattributes.
package gates

import (
	"bytes"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGofmtClean walks every .go file in the module and fails if any file
// differs from its gofmt (go/format.Source) rendering. Reading the files
// here also keys Go's test cache to their contents, so this cannot return
// a stale cached PASS after an unformatted file lands anywhere in the tree.
func TestGofmtClean(t *testing.T) {
	// Long tier (testsupport/tier.Long; this package may import nothing internal).
	if testing.Short() {
		t.Skip("long tier (check:server:long, release freeze): every .go file is gofmt-clean (the gate's gofmt step checks this everyday)")
	}
	root, rootErr := ModuleRoot()
	if rootErr != nil {
		t.Fatalf("gofmt gate: locate module root: %v", rootErr)
	}
	var offenders []string
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
		if !strings.HasSuffix(path, ".go") || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		formatted, fmtErr := format.Source(src)
		if fmtErr != nil {
			t.Logf("gofmt gate: skipping unparseable %s: %v", path, fmtErr)
			return nil
		}
		if !bytes.Equal(src, formatted) {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("gofmt gate walk failed: %v", err)
	}
	if len(offenders) > 0 {
		t.Errorf("%d file(s) are not gofmt-clean (CRLF endings count) - run `gofmt -w .` from the module root:\n\t%s",
			len(offenders), strings.Join(offenders, "\n\t"))
	}
}
