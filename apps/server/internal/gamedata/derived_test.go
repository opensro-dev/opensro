/*
===========================================================================

derived_test.go - an absent derived table is no error

===========================================================================
*/
package gamedata

import (
	"os"
	"path/filepath"
	"testing"
)

/*
================
TestReadDerived
================
*/
func TestReadDerived(t *testing.T) {
	dir := t.TempDir()
	if data, err := ReadDerived("", "world/table.json"); data != nil || err != nil {
		t.Fatalf("no derived folder: %q, %v", data, err)
	}
	if data, err := ReadDerived(dir, "world/table.json"); data != nil || err != nil {
		t.Fatalf("absent table: %q, %v", data, err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "world"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "world", "table.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if data, err := ReadDerived(dir, "world/table.json"); string(data) != "{}" || err != nil {
		t.Fatalf("present table: %q, %v", data, err)
	}
	if _, err := ReadDerived(dir, "world"); err == nil {
		t.Fatal("a folder read as a table")
	}
}
