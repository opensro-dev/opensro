package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FindModuleRoot locates this Go module from the working directory or the
// running executable. Source-checkout tools use it to anchor safe defaults;
// deployed processes should use explicit environment configuration.
func FindModuleRoot() (string, error) {
	starts := make([]string, 0, 2)
	if cwd, err := os.Getwd(); err == nil {
		starts = append(starts, cwd)
	}
	if executable, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(executable))
	}
	for _, start := range starts {
		for dir := filepath.Clean(start); ; dir = filepath.Dir(dir) {
			if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil &&
				!info.IsDir() {
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
	dotGit := filepath.Join(checkout, ".git")
	info, err := os.Stat(dotGit)
	if err != nil || info.IsDir() {
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
func WorktreeCopies(checkout, main string, lookup func(string) string) []string {
	if filepath.Clean(checkout) == filepath.Clean(main) {
		return nil
	}
	var copies []string
	for _, tree := range worktreeTrees {
		if lookup(tree.override) != "" {
			continue
		}
		path := filepath.Join(checkout, tree.tree)
		if _, err := os.Lstat(path); err == nil {
			copies = append(copies, path)
		}
	}
	return copies
}

/*
==================
RequireNoWorktreeCopies

The error a resolver returns when a worktree holds its own generated tree.
==================
*/
func RequireNoWorktreeCopies(checkout, main string) error {
	copies := WorktreeCopies(checkout, main, os.Getenv)
	if len(copies) == 0 {
		return nil
	}
	return fmt.Errorf("this worktree holds its own generated tree: %s; every tool reads and builds the main "+
		"checkout's (%s); move these aside into temp/ (unlink a symlink or junction, never delete through it) "+
		"and rerun", strings.Join(copies, ", "), main)
}
