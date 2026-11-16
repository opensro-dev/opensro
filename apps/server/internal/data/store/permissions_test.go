/*
===========================================================================

permissions_test.go - the authority directory's permission walk

The walk tightens the directory and its own files; a subdirectory another
owner keeps there (the crest store's marks, #3454) passes through it
untouched, so the next boot opens the store as before.

===========================================================================
*/
package store

import (
	"os"
	"path/filepath"
	"testing"
)

/*
================
TestAuthorityDirectoryKeepsASubdirectory
================
*/
func TestAuthorityDirectoryKeepsASubdirectory(t *testing.T) {
	dir := t.TempDir()
	marks := filepath.Join(dir, "marks")
	if err := os.MkdirAll(marks, 0o755); err != nil {
		t.Fatal(err)
	}
	crest := filepath.Join(marks, "crest.crb")
	if err := os.WriteFile(crest, []byte("crest"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureAuthorityDirectory(dir); err != nil {
		t.Fatalf("authority directory with a marks subdirectory: %v", err)
	}
	if info, err := os.Stat(marks); err != nil || !info.IsDir() {
		t.Fatalf("marks subdirectory after the walk: %v %v", info, err)
	}
	if data, err := os.ReadFile(crest); err != nil || string(data) != "crest" {
		t.Fatalf("crest after the walk: %q %v", data, err)
	}
}
