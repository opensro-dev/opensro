/*
===========================================================================

licensed.go - gate tests that need data derived from a licensed client

Many tests check the port against the real game data: the verified server
projection (apps/server/.generated/game-data), the raw client extraction
(extracted/ in the game root: SRO_GAME_ROOT or beside the main checkout)
or the published browser assets. That data is built locally from a client
the developer is permitted to use; it is never in the repository, so a
fresh clone and CI do not have it.

RequireGameData makes that explicit. Without the data a test skips with a
reason, so `pnpm check source` stays meaningful on a clean machine. Where the
data must be present - the full `pnpm check`, which sets
SRO_REQUIRE_GAME_DATA=1 - a missing piece fails the test instead, so nothing
is silently skipped where it matters.

Go's test cache validates only files inside the module. The projection is
inside it; the extraction and the published assets are not, so the gate
names their identity in SRO_LICENSED_DATA_IDENTITY and RequireGameData reads
it: a cached result is then reused only for the data it was produced from.

This package depends only on config within the module, so game-data tests
can use it without a dependency cycle.

===========================================================================
*/
package licensed

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"opensro.online/server/internal/config"
)

// RequireEnv makes missing game data a test failure instead of a skip.
const RequireEnv = "SRO_REQUIRE_GAME_DATA"

// IdentityEnv carries the identity of the licensed data outside the module
// (check_go_server.mjs sets it). Reading it makes it part of every licensed
// test's cache key.
const IdentityEnv = "SRO_LICENSED_DATA_IDENTITY"

/*
==================
RequireGameData

Skips (or, under SRO_REQUIRE_GAME_DATA=1, fails) the test unless the
verified server projection, the raw client extraction and the published
browser assets are all present. Call it first in any test that reads them.
A resolver error always fails: see gameDataVerdict.
==================
*/
func RequireGameData(t testing.TB) {
	t.Helper()
	missing, err := checkedGameData()
	switch verdict, reason := gameDataVerdict(missing, err, os.Getenv(RequireEnv) == "1"); verdict {
	case gameDataFail:
		t.Fatal(reason)
	case gameDataSkip:
		t.Skip(reason)
	}
}

// gameDataVerdict outcomes.
const (
	gameDataReady = iota
	gameDataSkip
	gameDataFail
)

/*
==================
gameDataVerdict

What RequireGameData does with one check. Only absent data may skip, and
only while SRO_REQUIRE_GAME_DATA is off. A resolver error is never absent
data: a worktree holding its own generated tree, an unreadable worktree
link or a relative override names a broken setup, and skipping on it let a
plain `go test ./...` in such a worktree pass with every licensed test
skipped.
==================
*/
func gameDataVerdict(missing []string, err error, require bool) (int, string) {
	if err != nil {
		return gameDataFail, "licensed game data cannot be located: " + err.Error()
	}
	if len(missing) == 0 {
		return gameDataReady, ""
	}
	reason := "licensed game data is not available"
	for _, path := range missing {
		reason += "\n\tmissing " + path
	}
	if require {
		return gameDataFail, fmt.Sprintf("%s (%s=1 requires it)", reason, RequireEnv)
	}
	return gameDataSkip, fmt.Sprintf("%s; build it with `pnpm assets build` (set %s=1 to fail instead of skip)",
		reason, RequireEnv)
}

var (
	checkOnce    sync.Once
	checkMissing []string
	checkErr     error
)

/*
==================
checkedGameData

missingGameData once per process, and the one read of IdentityEnv that puts
the licensed data's identity into the test cache key. Helpers call
RequireGameData inside per-row loops; checking the disk each time logged
~750k file operations for Go's test cache to re-validate on every run.
==================
*/
func checkedGameData() ([]string, error) {
	checkOnce.Do(func() {
		_ = os.Getenv(IdentityEnv)
		checkMissing, checkErr = missingGameData()
	})
	return checkMissing, checkErr
}

// GeneratedRootEnv names a shared generated tree; scripts/lib/generatedRoot.mjs
// owns the rule for the scripts and the client, this is its Go side.
const GeneratedRootEnv = "SRO_GENERATED_ROOT"

/*
==================
ClientPublicRoot

The published client tree: client-public under SRO_GENERATED_ROOT when it is
set (it must be absolute, as for the scripts), else the main checkout's
.generated/client-public. A worktree then reads the shared build with no
junction and no environment.
==================
*/
func ClientPublicRoot() (string, error) {
	if err := config.RequireSourceWorktreeClean(); err != nil {
		return "", err
	}
	if root := os.Getenv(GeneratedRootEnv); root != "" {
		if !filepath.IsAbs(root) {
			return "", fmt.Errorf("%s must be an absolute path, not %q", GeneratedRootEnv, root)
		}
		return filepath.Join(root, "client-public"), nil
	}
	main, err := mainRepositoryRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(main, ".generated", "client-public"), nil
}

/*
==================
missingGameData

Returns the data locations that do not exist. Explicit environment roots win
over the checkout layout, matching how the server resolves them.
==================
*/
func missingGameData() ([]string, error) {
	repository, err := repositoryRoot()
	if err != nil {
		return nil, err
	}
	gameRoot, err := resolveGameRoot(repository)
	if err != nil {
		return nil, err
	}
	projection := os.Getenv("SRO_SERVER_GAME_DATA_ROOT")
	if projection == "" {
		main, mainErr := mainRepositoryRoot()
		if mainErr != nil {
			return nil, mainErr
		}
		projection = filepath.Join(main, "apps", "server", ".generated", "game-data", "1.150", "server", "manifest.json")
	}
	publicRoot, err := ClientPublicRoot()
	if err != nil {
		return nil, err
	}
	required := []string{
		projection,
		filepath.Join(gameRoot, "extracted", "Media_extracted"),
		filepath.Join(publicRoot, "assets", "packs", "manifest.json"),
	}
	return missingPaths(required, os.Stat)
}

/*
================
missingPaths

Only absence may skip licensed tests. Preserve inspection errors so the
gate inherited from PR349 fails them regardless of RequireEnv.
================
*/
func missingPaths(required []string, stat func(string) (os.FileInfo, error)) ([]string, error) {
	var missing []string
	for _, path := range required {
		if _, statErr := stat(path); errors.Is(statErr, os.ErrNotExist) {
			missing = append(missing, filepath.Clean(path))
		} else if statErr != nil {
			return nil, fmt.Errorf("inspect licensed data %s: %w", path, statErr)
		}
	}
	return missing, nil
}

/*
==================
RetailTextdataDir

The raw client textdata of the extraction
(<game root>/extracted/Media_extracted/server_dep/silkroad/textdata), for
tests that check the port against the shipped tables rather than the
verified server projection. The caller keeps its own skip or
RequireGameData check; this only names the folder, the one rule every tool
uses to find the game root.
==================
*/
func RetailTextdataDir(t testing.TB) string {
	t.Helper()
	repository, err := repositoryRoot()
	if err != nil {
		t.Fatalf("locate the repository: %v", err)
	}
	gameRoot, err := resolveGameRoot(repository)
	if err != nil {
		t.Fatalf("locate the game root: %v", err)
	}
	return filepath.Join(gameRoot, "extracted", "Media_extracted", "server_dep", "silkroad", "textdata")
}

/*
==================
resolveGameRoot

The directory holding extracted/, by the rule of scripts/build/world/paths.mjs
and scripts/sro_paths.py: SRO_GAME_ROOT when set, else the parent of the main
checkout (config.MainCheckoutRoot).
==================
*/
func resolveGameRoot(repository string) (string, error) {
	main, err := config.MainCheckoutRoot(repository)
	if err != nil {
		return "", err
	}
	if err := config.RequireNoWorktreeCopies(repository, main); err != nil {
		return "", err
	}
	if configured := strings.TrimSpace(os.Getenv("SRO_GAME_ROOT")); configured != "" {
		return filepath.Abs(configured)
	}
	return filepath.Join(main, ".."), nil
}

/*
==================
repositoryRoot

The rebuild checkout: two levels above the Go module (apps/server).
==================
*/
func repositoryRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		filename := filepath.Join(dir, "go.mod")
		if _, statErr := os.Stat(filename); statErr == nil {
			return filepath.Join(dir, "..", ".."), nil
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return "", fmt.Errorf("inspect module marker %s: %w", filename, statErr)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no go.mod above the test's working directory")
		}
		dir = parent
	}
}

/*
==================
mainRepositoryRoot

The main checkout of the rebuild checkout this test runs in, where the
built trees live (config.MainCheckoutRoot).
==================
*/
func mainRepositoryRoot() (string, error) {
	repository, err := repositoryRoot()
	if err != nil {
		return "", err
	}
	main, err := config.MainCheckoutRoot(repository)
	if err != nil {
		return "", err
	}
	return main, config.RequireNoWorktreeCopies(repository, main)
}
