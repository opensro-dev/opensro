/*
===========================================================================

module_root.go - locate source roots and reject unowned worktree outputs

Explicit deployment roots do not require a source checkout. Inside one,
each override exempts only its own generated tree. Filesystem failures are
errors, never evidence that a checkout or generated tree is absent.

===========================================================================
*/
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

/*
================
FindModuleRoot

Locate the module from the working directory or running executable.
================
*/
func FindModuleRoot() (string, error) {
	starts := make([]string, 0, 2)
	if cwd, err := os.Getwd(); err == nil {
		starts = append(starts, cwd)
	}
	if executable, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(executable))
	}
	return findModuleRoot(starts, os.Stat)
}

/*
================
findModuleRoot
================
*/
func findModuleRoot(starts []string, stat func(string) (os.FileInfo, error)) (string, error) {
	for _, start := range starts {
		for dir := filepath.Clean(start); ; dir = filepath.Dir(dir) {
			filename := filepath.Join(dir, "go.mod")
			info, err := stat(filename)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("inspect module marker %s: %w", filename, err)
			}
			if err == nil && !info.IsDir() {
				return dir, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
		}
	}
	return "", fmt.Errorf("could not locate the server module root")
}

/*
==================
MainCheckoutRoot

The main checkout for a checkout root: the checkout itself, or the one a
linked git worktree's .git file names ("gitdir: <main>/.git/worktrees/<name>";
a main checkout's .git is a directory). The built trees live there, so a
worktree reads them with no environment. scripts/lib/generatedRoot.mjs and
scripts/sro_paths.py hold the same rule.
==================
*/
func MainCheckoutRoot(checkout string) (string, error) {
	return mainCheckoutRoot(checkout, os.Stat)
}

/*
================
mainCheckoutRoot
================
*/
func mainCheckoutRoot(checkout string, stat func(string) (os.FileInfo, error)) (string, error) {
	dotGit := filepath.Join(checkout, ".git")
	info, err := stat(dotGit)
	if errors.Is(err, os.ErrNotExist) {
		return filepath.Clean(checkout), nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect checkout marker %s: %w", dotGit, err)
	}
	if info.IsDir() {
		return filepath.Clean(checkout), nil
	}
	link, err := os.ReadFile(dotGit)
	if err != nil {
		return "", err
	}
	for line := range strings.SplitSeq(string(link), "\n") {
		if target, ok := strings.CutPrefix(strings.TrimSpace(line), "gitdir:"); ok {
			worktreeGitDir := strings.TrimSpace(target)
			if !filepath.IsAbs(worktreeGitDir) {
				worktreeGitDir = filepath.Join(checkout, worktreeGitDir)
			}
			// <main>/.git/worktrees/<name> -> <main>
			return filepath.Join(worktreeGitDir, "..", "..", ".."), nil
		}
	}
	return "", fmt.Errorf("unreadable worktree link %s", dotGit)
}

// worktreeTrees are the generated trees a linked worktree must never hold
// itself, each with the override that names another tree explicitly.
var worktreeTrees = []struct{ tree, override string }{
	{".generated", "SRO_GENERATED_ROOT"},
	{filepath.Join("apps", "server", ".generated"), "SRO_SERVER_GAME_DATA_ROOT"},
}

/*
==================
WorktreeCopies

The generated trees a linked worktree holds of its own - a copy, a symlink
or a junction, broken or not - unless that tree's override is set (lookup
reads the environment). The main checkout's trees are the only ones any
tool reads and builds, so a second one is a stale tree waiting to be used.
Empty for the main checkout. scripts/lib/generatedRoot.mjs and
scripts/sro_paths.py hold the same rule.
==================
*/
func WorktreeCopies(checkout, main string, lookup func(string) string) ([]string, error) {
	return worktreeCopies(checkout, main, lookup, os.Lstat)
}

/*
================
worktreeCopies
================
*/
func worktreeCopies(checkout, main string, lookup func(string) string, lstat func(string) (os.FileInfo, error)) ([]string, error) {
	if filepath.Clean(checkout) == filepath.Clean(main) {
		return nil, nil
	}
	var copies []string
	for _, tree := range worktreeTrees {
		if lookup(tree.override) != "" {
			continue
		}
		path := filepath.Join(checkout, tree.tree)
		if _, err := lstat(path); err == nil {
			copies = append(copies, path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect worktree tree %s: %w", path, err)
		}
	}
	return copies, nil
}

/*
==================
RequireNoWorktreeCopies

The error a resolver returns when a worktree holds its own generated tree.
==================
*/
func RequireNoWorktreeCopies(checkout, main string) error {
	copies, err := WorktreeCopies(checkout, main, os.Getenv)
	if err != nil {
		return err
	}
	if len(copies) == 0 {
		return nil
	}
	return fmt.Errorf("this worktree holds its own generated tree: %s; every tool reads and builds the main "+
		"checkout's (%s); move these aside into temp/ (unlink a symlink or junction, never delete through it) "+
		"and rerun", strings.Join(copies, ", "), main)
}

/*
================
RequireSourceWorktreeClean

Check the working directory's checkout even when a resolver has an explicit
root. No .git ancestor means a deployed process, which needs no checkout.
Do not search beside the executable: an external deployment can use a
binary built in a worktree without depending on that source tree.
================
*/
func RequireSourceWorktreeClean() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("locate working directory: %w", err)
	}
	return requireSourceWorktreeClean(cwd, os.Lstat)
}

/*
================
requireSourceWorktreeClean
================
*/
func requireSourceWorktreeClean(start string, lstat func(string) (os.FileInfo, error)) error {
	for dir := filepath.Clean(start); ; dir = filepath.Dir(dir) {
		marker := filepath.Join(dir, ".git")
		if _, err := lstat(marker); err == nil {
			main, err := MainCheckoutRoot(dir)
			if err != nil {
				return err
			}
			return RequireNoWorktreeCopies(dir, main)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect checkout marker %s: %w", marker, err)
		}
		if filepath.Dir(dir) == dir {
			return nil
		}
	}
}
