/*
===========================================================================

publication_test.go - skill-object visibility and reliable retry boundaries

Publication depends on admitted transport GIDs and exact population leases,
not on whether a previous tick attempted to enqueue a frame.

===========================================================================
*/
package skillobject

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
)

/*
================
TestTrapPublicationRetriesAndRetiresAcrossWorldBoundaries
================
*/
func TestTrapPublicationRetriesAndRetiresAcrossWorldBoundaries(t *testing.T) {
	var registry Registry
	object := trapFixture(t, &registry)
	viewer := Viewer{Division: object.Division, Population: object.Population,
		Position: worldgeom.RegionXZ{RegionID: object.Spawn.Region, X: 100, Z: 200}}
	if frames := ScopeFrames(registry.Snapshot(), viewer); len(frames) != 0 {
		t.Fatal("published before scene admission")
	}
	viewer.Published = []uint32{}
	for attempt := 0; attempt < 2; attempt++ {
		frames := ScopeFrames(registry.Snapshot(), viewer)
		if len(frames) != 1 || frames[0].Opcode != wire.OpSingleObjectSpawn || !frames[0].Scope[0].Visible {
			t.Fatal("unadmitted spawn was not retried", frames)
		}
	}
	viewer.Published = []uint32{object.Spawn.GID}
	if frames := ScopeFrames(registry.Snapshot(), viewer); len(frames) != 0 {
		t.Fatal("admitted spawn repeated")
	}
	viewer.Population.Generation++
	frames := ScopeFrames(registry.Snapshot(), viewer)
	if len(frames) != 1 || frames[0].Opcode != wire.OpObjectDespawn || frames[0].Scope[0].Visible {
		t.Fatal("recycled world retained old trap", frames)
	}
	viewer.Population = object.Population
	registry.Remove(object.Spawn.GID)
	if frames := ScopeFrames(registry.Snapshot(), viewer); len(frames) != 1 || frames[0].Opcode != wire.OpObjectDespawn {
		t.Fatal("retired trap remained published", frames)
	}
}
