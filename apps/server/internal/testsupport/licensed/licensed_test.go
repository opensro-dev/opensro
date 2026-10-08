/*
===========================================================================

licensed_test.go - the licensed gate finds the game root like the build

The asset build (scripts/build/world/paths.mjs) and the Python tools
(scripts/sro_paths.py) look for extracted/ in SRO_GAME_ROOT or beside the
main checkout. The gate must look in the same place, or a checkout the
build accepts skips its licensed tests. Only absent data may skip: a
setup the resolvers refuse fails the test.

===========================================================================
*/
package licensed

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opensro.online/server/internal/config"
)

/*
================
TestResolveGameRoot
================
*/
func TestResolveGameRoot(t *testing.T) {
	workspace := t.TempDir()
	main := filepath.Join(workspace, "OpenSRO")
	if err := os.MkdirAll(filepath.Join(main, ".git", "worktrees", "feature"), 0o755); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(workspace, "elsewhere", "feature")
	if err := os.MkdirAll(linked, 0o755); err != nil {
		t.Fatal(err)
	}
	link := "gitdir: " + filepath.Join(main, ".git", "worktrees", "feature") + "\n"
	if err := os.WriteFile(filepath.Join(linked, ".git"), []byte(link), 0o644); err != nil {
		t.Fatal(err)
	}
	configured := filepath.Join(workspace, "client")

	cases := []struct {
		name       string
		repository string
		env        string
		want       string
	}{
		{"main checkout", main, "", workspace},
		{"linked worktree", linked, "", workspace},
		{"SRO_GAME_ROOT", linked, configured, configured},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("SRO_GAME_ROOT", c.env)
			got, err := resolveGameRoot(c.repository)
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Clean(got) != filepath.Clean(c.want) {
				t.Fatalf("game root %s, want %s", got, c.want)
			}
		})
	}
}

/*
================
TestGameDataVerdict

Absent data skips, or fails under SRO_REQUIRE_GAME_DATA=1. A resolver
error fails either way, including the worktree guard's real refusal.
================
*/
func TestGameDataVerdict(t *testing.T) {
	workspace := t.TempDir()
	main := filepath.Join(workspace, "main")
	worktree := filepath.Join(workspace, "wt")
	for _, dir := range []string{filepath.Join(main, ".git", "worktrees", "wt"), filepath.Join(worktree, ".generated")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("SRO_GENERATED_ROOT", "")
	t.Setenv("SRO_SERVER_GAME_DATA_ROOT", "")
	guard := config.RequireNoWorktreeCopies(worktree, main)
	if guard == nil {
		t.Fatal("the worktree guard accepted a worktree holding its own .generated")
	}
	missing := []string{filepath.Join(workspace, "extracted")}
	cases := []struct {
		name    string
		missing []string
		err     error
		require bool
		want    int
	}{
		{"present", nil, nil, false, gameDataReady},
		{"absent", missing, nil, false, gameDataSkip},
		{"absent, required", missing, nil, true, gameDataFail},
		{"worktree guard", nil, guard, false, gameDataFail},
		{"worktree guard with absent data", missing, guard, false, gameDataFail},
		{"unreadable link", nil, errors.New("unreadable worktree link"), false, gameDataFail},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, reason := gameDataVerdict(c.missing, c.err, c.require)
			if got != c.want {
				t.Fatalf("verdict %d, want %d (%s)", got, c.want, reason)
			}
			if c.err != nil && !strings.Contains(reason, c.err.Error()) {
				t.Fatalf("reason %q does not name the resolver error", reason)
			}
		})
	}
}

/*
================
TestMissingPathsInspection

The real discovery helper must propagate failures to the PR349 verdict;
testing the verdict alone cannot catch an error swallowed during stat.
================
*/
func TestMissingPathsInspection(t *testing.T) {
	root := t.TempDir()
	present := filepath.Join(root, "present")
	if err := os.WriteFile(present, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	absent := filepath.Join(root, "absent")
	missing, err := missingPaths([]string{present, absent}, os.Stat)
	if err != nil || len(missing) != 1 || missing[0] != absent {
		t.Fatalf("missing paths = %v, %v", missing, err)
	}
	for _, cause := range []error{os.ErrPermission, errors.New("device read failure")} {
		stat := func(name string) (os.FileInfo, error) {
			if name == present {
				return nil, &os.PathError{Op: "stat", Path: name, Err: cause}
			}
			return os.Stat(name)
		}
		missing, err := missingPaths([]string{absent, present}, stat)
		if !errors.Is(err, cause) || !strings.Contains(err.Error(), present) {
			t.Fatalf("inspection error = %v; want cause and path", err)
		}
		if verdict, _ := gameDataVerdict(missing, err, false); verdict != gameDataFail {
			t.Fatalf("inspection failure became verdict %d", verdict)
		}
	}
}

/*
================
TestExplicitClientRootStillGuardsServerTree
================
*/
func TestExplicitClientRootStillGuardsServerTree(t *testing.T) {
	workspace := t.TempDir()
	main := filepath.Join(workspace, "main")
	worktree := filepath.Join(workspace, "wt")
	module := filepath.Join(worktree, "apps", "server")
	for _, dir := range []string{filepath.Join(main, ".git", "worktrees", "wt"), filepath.Join(module, ".generated")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: "+filepath.Join(main, ".git", "worktrees", "wt")), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(module)
	t.Setenv(GeneratedRootEnv, filepath.Join(workspace, "shared"))
	t.Setenv("SRO_SERVER_GAME_DATA_ROOT", "")
	t.Setenv("SRO_GAME_ROOT", filepath.Join(workspace, "retail"))
	if _, err := ClientPublicRoot(); err == nil || !strings.Contains(err.Error(), filepath.Join(module, ".generated")) {
		t.Fatalf("explicit client root bypassed server guard: %v", err)
	}
	if _, err := resolveGameRoot(worktree); err == nil || !strings.Contains(err.Error(), "own generated tree") {
		t.Fatalf("explicit retail root bypassed guard: %v", err)
	}
	t.Setenv("SRO_SERVER_GAME_DATA_ROOT", filepath.Join(workspace, "projection"))
	if got, err := ClientPublicRoot(); err != nil || got != filepath.Join(workspace, "shared", "client-public") {
		t.Fatalf("both trees overridden: %q, %v", got, err)
	}
}

/*
================
TestExplicitClientRootOutsideCheckout
================
*/
func TestExplicitClientRootOutsideCheckout(t *testing.T) {
	workspace := t.TempDir()
	t.Chdir(workspace)
	t.Setenv(GeneratedRootEnv, filepath.Join(workspace, "shared"))
	t.Setenv("SRO_SERVER_GAME_DATA_ROOT", "")
	if got, err := ClientPublicRoot(); err != nil || got != filepath.Join(workspace, "shared", "client-public") {
		t.Fatalf("external client root = %q, %v", got, err)
	}
}
