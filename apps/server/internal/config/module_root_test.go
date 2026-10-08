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
	// Clean: a GOTMPDIR spelled with forward slashes on Windows yields a mixed
	// path, and MainCheckoutRoot returns clean ones.
	scratch := filepath.Clean(t.TempDir())
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

/*
================
TestWorktreeCopiesFindsCopiesUnlessOverridden

A linked worktree's own .generated or apps/server/.generated is reported;
each tree's override exempts only that tree; the main checkout owns its own.
================
*/
func TestWorktreeCopiesFindsCopiesUnlessOverridden(t *testing.T) {
	scratch := t.TempDir()
	main := filepath.Join(scratch, "main")
	worktree := filepath.Join(scratch, "wt")
	for _, dir := range []string{
		filepath.Join(main, ".git", "worktrees", "wt"),
		filepath.Join(worktree, ".generated"),
		filepath.Join(worktree, "apps", "server", ".generated"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	none := func(string) string { return "" }
	got := WorktreeCopies(worktree, main, none)
	want := []string{filepath.Join(worktree, ".generated"), filepath.Join(worktree, "apps", "server", ".generated")}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("WorktreeCopies = %v, want %v", got, want)
	}
	shared := func(name string) string {
		if name == "SRO_GENERATED_ROOT" {
			return filepath.Join(scratch, "elsewhere")
		}
		return ""
	}
	if got := WorktreeCopies(worktree, main, shared); len(got) != 1 || got[0] != want[1] {
		t.Fatalf("an override exempts only its own tree: %v", got)
	}
	if got := WorktreeCopies(main, main, none); len(got) != 0 {
		t.Fatalf("the main checkout owns its trees: %v", got)
	}
}
