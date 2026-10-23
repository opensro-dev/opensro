/*
===========================================================================

serverfixture.go - reference fixtures traced from the server, in source-data

Fixtures recorded against the original server are the operator's evidence,
never in the repository. They live in source-data/fixtures/server under
their package's path in apps/server (internal/game/combat/testdata/...).
A test that needs one skips without it, or fails under
SRO_REQUIRE_GAME_DATA=1, as SourceDataFile does.

===========================================================================
*/
package licensed

import (
	"bytes"
	"io"
	"testing"
)

// serverFixtureRoot is the fixtures' folder under source-data.
const serverFixtureRoot = "fixtures/server/"

/*
================
ServerFixture

The fixture at name, a path under apps/server. The error is always nil:
the shape matches the os.ReadFile it replaces.
================
*/
func ServerFixture(t testing.TB, name string) ([]byte, error) {
	t.Helper()
	return SourceDataFile(t, serverFixtureRoot+name), nil
}

/*
================
ServerFixtureReader

ServerFixture as a reader, for the os.Open it replaces.
================
*/
func ServerFixtureReader(t testing.TB, name string) (io.ReadCloser, error) {
	t.Helper()
	data, err := ServerFixture(t, name)
	return io.NopCloser(bytes.NewReader(data)), err
}

/*
================
SHA256Hex

Whether s is a lowercase sha256 hex digest: a server fixture names the
binary it was traced from, which the operator's copy must still carry.
================
*/
func SHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
