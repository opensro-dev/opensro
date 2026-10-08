/*
===========================================================================

module_root_test.go - tests for module_root.go

===========================================================================
*/
package config

import (
	"os"
	"path/filepath"
	"testing"
)

/*
================
TestMainCheckoutRootFollowsAWorktreeLink

A linked worktree's .git file names the main checkout; a main checkout's
.git directory, or no .git at all, is the checkout itself; a .git file
without a gitdir line is an error rather than a silent guess.
================
*/
func TestMainCheckoutRootFollowsAWorktreeLink(t *testing.T) {
	scratch := t.TempDir()
	main := filepath.Join(scratch, "main")
	worktree := filepath.Join(scratch, "wt")
	if err := os.MkdirAll(filepath.Join(main, ".git", "worktrees", "wt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	link := []byte("gitdir: " + filepath.Join(main, ".git", "worktrees", "wt") + "\n")
	if err := os.WriteFile(filepath.Join(worktree, ".git"), link, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, checkout := range []string{worktree, main, scratch} {
		want := main
		if checkout == scratch {
			want = scratch
		}
		if got, err := MainCheckoutRoot(checkout); err != nil || got != want {
			t.Fatalf("MainCheckoutRoot(%s) = %q, %v; want %q", checkout, got, err, want)
		}
	}
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("not a link\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := MainCheckoutRoot(worktree); err == nil {
		t.Fatal("a .git file without a gitdir line was accepted")
	}
}
