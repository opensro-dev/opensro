/*
===========================================================================

admission_bench_test.go - how long an entry bootstrap holds its gate

EnterWorld holds the division's publication gate from scene admission
through the bootstrap batch (register.go). The action tick waits for that
gate between its steps (#713), so this is the longest a tick step can be
delayed by one login.

===========================================================================
*/
package enterworld

import (
	"testing"

	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/transport"
)

/*
================
BenchmarkHandleEnterWorld

One complete admission against the shipped skill data.
================
*/
func BenchmarkHandleEnterWorld(b *testing.B) {
	clientdata.RequireGameData(b)
	source := NewTextdataSkills(gamedatatest.TextdataDir(b))
	refs, err := NewBrowserReferences(BrowserReferenceSources{Skills: source})
	if err != nil {
		b.Fatal(err)
	}
	character := chinaSpearman()
	deps := testDeps(character)
	deps.Skills = source
	deps.BrowserReferences = refs
	for b.Loop() {
		b.StopTimer()
		request := transport.EncodeEnterWorld(entryauth.NewAuthenticatedEntryFixture(b, DefaultDivisionID, character.Name))
		b.StartTimer()
		if outcome := HandleEnterWorld(deps, request); !outcome.OK {
			b.Fatalf("admission failed: %+v", outcome.Result)
		}
	}
}
