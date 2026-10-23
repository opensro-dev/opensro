/*
===========================================================================

derived_test.go - the operator's quest catalogues for this package's tests

The generated and compiled quest catalogues are source-data, never in the
repository. Tests over the real textdata read them from
<game root>/source-data/server, which has the derived data's layout.

===========================================================================
*/
package quest

import (
	"testing"

	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
derivedForTests
================
*/
func derivedForTests(t testing.TB) string {
	t.Helper()
	dir, err := licensed.SourceDataServerDir()
	if err != nil {
		t.Fatalf("locate source-data: %v", err)
	}
	return dir
}
