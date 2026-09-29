/*
===========================================================================

gamedatatest.go - the verified game-data projection for tests

Tests locate the server projection through gamedata.Resolve, the same owner
the server uses, never through a path of their own. A hand-counted
"../../../../.." breaks when the projection or the package moves, and did:
several tests climbed out of the repository and silently read nothing.

Each accessor first gates on licensed.RequireGameData, so a machine without
the licensed data skips (or, under SRO_REQUIRE_GAME_DATA=1, fails) instead
of erroring deep inside a loader.

The projection resolves once per test process. gamedata.Resolve
re-identifies the artifact on disk on every call, which a long-running
server wants; a test binary calling it per test (quest: 201 times) turned
each call's file checks into thousands of lines Go's test cache
re-validates on every cached run.

===========================================================================
*/
package gamedatatest

import (
	"sync"
	"testing"

	"opensro.online/server/internal/gamedata"
	"opensro.online/server/internal/testsupport/licensed"
)

var (
	resolveOnce  sync.Once
	resolved     gamedata.Paths
	errResolving error
)

/*
================
resolve

gamedata.Resolve, once per process.
================
*/
func resolve() (gamedata.Paths, error) {
	resolveOnce.Do(func() {
		resolved, errResolving = gamedata.Resolve()
	})
	return resolved, errResolving
}

/*
================
Paths

The verified projection, or the test stops.
================
*/
func Paths(t testing.TB) gamedata.Paths {
	t.Helper()
	licensed.RequireGameData(t)
	paths, err := resolve()
	if err != nil {
		t.Fatalf("resolve server game-data projection: %v", err)
	}
	return paths
}

/*
================
TextdataDir
================
*/
func TextdataDir(t testing.TB) string {
	t.Helper()
	return Paths(t).TextdataDir
}

/*
================
WorldAuthorityDir
================
*/
func WorldAuthorityDir(t testing.TB) string {
	t.Helper()
	return Paths(t).WorldAuthorityDir
}

/*
================
TextdataDirOrEmpty

For lazily built shared fixtures whose loaders run outside a test's scope
(called on first use, inside a test - never at package initialization,
which precedes the test log): the verified textdata directory, or "" when
the projection is absent. Loaders given ""
report missing data when a test uses them, and such tests call
licensed.RequireGameData first.
================
*/
func TextdataDirOrEmpty() string {
	paths, err := resolve()
	if err != nil {
		return ""
	}
	return paths.TextdataDir
}
