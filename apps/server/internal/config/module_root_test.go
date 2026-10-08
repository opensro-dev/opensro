/*
===========================================================================

module_root_test.go - tests for module_root.go

===========================================================================
*/
package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	got, err := WorktreeCopies(worktree, main, none)
	want := []string{filepath.Join(worktree, ".generated"), filepath.Join(worktree, "apps", "server", ".generated")}
	if err != nil || len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("WorktreeCopies = %v, want %v", got, want)
	}
	shared := func(name string) string {
		if name == "SRO_GENERATED_ROOT" {
			return filepath.Join(scratch, "elsewhere")
		}
		return ""
	}
	if got, err := WorktreeCopies(worktree, main, shared); err != nil || len(got) != 1 || got[0] != want[1] {
		t.Fatalf("an override exempts only its own tree: %v", got)
	}
	if got, err := WorktreeCopies(main, main, none); err != nil || len(got) != 0 {
		t.Fatalf("the main checkout owns its trees: %v", got)
	}
}

/*
================
TestRootInspectionErrors

Injected errors keep permission and I/O coverage portable, without chmod
or machine-specific ACLs. Absence remains a valid non-checkout result.
================
*/
func TestRootInspectionErrors(t *testing.T) {
	root := filepath.Clean(t.TempDir())
	for _, cause := range []error{os.ErrPermission, errors.New("device read failure")} {
		stat := func(name string) (os.FileInfo, error) {
			return nil, &os.PathError{Op: "stat", Path: name, Err: cause}
		}
		checks := []struct {
			name string
			run  func() error
		}{
			{"module", func() error { _, err := findModuleRoot([]string{root}, stat); return err }},
			{"checkout", func() error { _, err := mainCheckoutRoot(root, stat); return err }},
			{"source guard", func() error { return requireSourceWorktreeClean(root, stat) }},
			{"tree", func() error {
				_, err := worktreeCopies(root, filepath.Join(root, "main"), func(string) string { return "" }, stat)
				return err
			}},
		}
		for _, check := range checks {
			t.Run(check.name+"/"+cause.Error(), func(t *testing.T) {
				err := check.run()
				if !errors.Is(err, cause) || !strings.Contains(err.Error(), root) {
					t.Fatalf("inspection error = %v; want cause and path", err)
				}
			})
		}
	}
	absent := func(name string) (os.FileInfo, error) {
		return nil, &os.PathError{Op: "stat", Path: name, Err: os.ErrNotExist}
	}
	if got, err := mainCheckoutRoot(root, absent); err != nil || got != filepath.Clean(root) {
		t.Fatalf("absent checkout = %q, %v", got, err)
	}
	if err := requireSourceWorktreeClean(root, absent); err != nil {
		t.Fatalf("external deployment: %v", err)
	}
}
