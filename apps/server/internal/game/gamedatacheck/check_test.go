/*
===========================================================================

check_test.go - the release preflight accepts the shipped data, refuses
data it cannot start on

===========================================================================
*/

package gamedatacheck

import (
	"strings"
	"testing"

	"opensro.online/server/internal/gamedata"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestCheckAcceptsTheShippedGameData
================
*/
func TestCheckAcceptsTheShippedGameData(t *testing.T) {
	licensed.RequireGameData(t)
	result, err := Check()
	if err != nil {
		t.Fatalf("shipped game data refused: %v", err)
	}
	if !strings.HasPrefix(result.ManifestDigest, "sha256:") {
		t.Fatalf("manifest digest = %q", result.ManifestDigest)
	}
}

/*
================
TestCheckRefusesDataTheServerCannotOpen

A root without a verified projection is refused before any loader runs,
with the reason in the error the deploy prints.
================
*/
func TestCheckRefusesDataTheServerCannotOpen(t *testing.T) {
	t.Setenv(gamedata.EnvRoot, t.TempDir())
	if _, err := Check(); err == nil || !strings.Contains(err.Error(), "game data:") {
		t.Fatalf("empty root: err = %v", err)
	}
}
