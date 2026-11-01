/*
===========================================================================

reentry_bench_test.go - what one teleport re-entry bootstrap costs

completeReturnScroll rebuilds the entry through ReentryPackets on the
tick, under the division lock and every publication gate (#1820). This
bench is that rebuild: Build plus the browser envelope, against a
reference snapshot the size of the shipped one.

===========================================================================
*/
package enterworld

import (
	"fmt"
	"testing"
)

/*
================
setupReentryBenchCache

Gives the fixture the filtered-snapshot cache the production
constructor (NewDevDepsWithRoster) installs.
================
*/
func setupReentryBenchCache(deps *Deps) {
	deps.refObjCache = &refObjSnapshotCache{}
}

/*
================
BenchmarkReentryPackets

One full re-entry packet rebuild for an already-bound character.
================
*/
func BenchmarkReentryPackets(b *testing.B) {
	character := chinaSpearman()
	deps := testDeps(character)
	// The production snapshot (devdeps.go) rebuilds the playable model
	// catalogue, the NPC roster, the summonable refs and the spawnable
	// monster roster on every call; 600 rows is that size.
	deps.RefObjSnapshot = func() []RefObjRow {
		rows := make([]RefObjRow, 0, 600)
		for i := 0; i < 600; i++ {
			rows = append(rows, RefObjRow{RefObjID: uint32(1000 + i), Codename: fmt.Sprintf("MOB_BENCH_%03d", i), Kind: "monster"})
		}
		return rows
	}
	setupReentryBenchCache(deps)
	if _, ok := deps.ReentryPackets(DefaultDivisionID, character.Name); !ok {
		b.Fatal("fixture re-entry refused")
	}
	b.ResetTimer()
	for b.Loop() {
		packets, ok := deps.ReentryPackets(DefaultDivisionID, character.Name)
		if !ok || len(packets) == 0 {
			b.Fatal("re-entry refused")
		}
	}
}
