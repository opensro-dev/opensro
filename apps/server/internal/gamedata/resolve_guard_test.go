/*
===========================================================================

resolve_guard_test.go - explicit projections still enforce source ownership

Use a verified synthetic bundle and a scratch worktree. No licensed inputs
or filesystem permission changes are required.

===========================================================================
*/
package gamedata

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

/*
================
TestExplicitProjectionGuardsClientTree
================
*/
func TestExplicitProjectionGuardsClientTree(t *testing.T) {
	root, digest := writeTestBundle(t, nil)
	workspace := t.TempDir()
	main := filepath.Join(workspace, "main")
	worktree := filepath.Join(workspace, "wt")
	module := filepath.Join(worktree, "apps", "server")
	for _, dir := range []string{filepath.Join(main, ".git", "worktrees", "wt"), filepath.Join(worktree, ".generated"), module} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: "+filepath.Join(main, ".git", "worktrees", "wt")), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(module)
	t.Setenv(EnvRoot, root)
	t.Setenv(EnvManifestDigest, digest)
	t.Setenv("SRO_GENERATED_ROOT", "")
	if _, err := Resolve(); err == nil || !strings.Contains(err.Error(), filepath.Join(worktree, ".generated")) {
		t.Fatalf("explicit projection bypassed client guard: %v", err)
	}
	t.Setenv("SRO_GENERATED_ROOT", filepath.Join(workspace, "shared"))
	if got, err := Resolve(); err != nil || got.BundleRoot != root {
		t.Fatalf("both trees overridden: %+v, %v", got, err)
	}
	// Guard admission precedes the verified-artifact cache on every call.
	t.Setenv("SRO_GENERATED_ROOT", "")
	if _, err := Resolve(); err == nil || !strings.Contains(err.Error(), "own generated tree") {
		t.Fatalf("warm projection bypassed client guard: %v", err)
	}
}

/*
================
TestExplicitProjectionOutsideCheckout

An explicit production artifact needs neither go.mod nor Git metadata.
================
*/
func TestExplicitProjectionOutsideCheckout(t *testing.T) {
	root, digest := writeTestBundle(t, nil)
	t.Chdir(t.TempDir())
	t.Setenv(EnvRoot, root)
	t.Setenv(EnvManifestDigest, digest)
	t.Setenv("SRO_GENERATED_ROOT", "")
	if got, err := Resolve(); err != nil || got.BundleRoot != root {
		t.Fatalf("external projection: %+v, %v", got, err)
	}
}
