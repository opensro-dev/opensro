/*
===========================================================================

licensed_test.go - the licensed gate finds the game root like the build

The asset build (scripts/build/world/paths.mjs) and the Python tools
(scripts/sro_paths.py) look for extracted/ in SRO_GAME_ROOT or beside the
main checkout. The gate must look in the same place, or a checkout the
build accepts skips its licensed tests.

===========================================================================
*/
package licensed

import (
	"os"
	"path/filepath"
	"testing"
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
