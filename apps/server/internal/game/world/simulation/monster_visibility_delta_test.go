/*
===========================================================================

monster_visibility_delta_test.go - simulation monster  visibility delta test ownership

===========================================================================
*/

package simulation

import (
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
	"testing"
)

/*
================
TestArchivedMonsterMetadataAndKnownVisibilityAvoidBackingReads
================
*/
func TestArchivedMonsterMetadataAndKnownVisibilityAvoidBackingReads(t *testing.T) {
	s := NewMonsterState(monster.Template{})
	s.StartDivision("world")
	if err := s.EnableDormantStorage(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	d := s.divs["world"]
	d.movers = newMoverStorage(nil)
	actor := monster.Instance{Gid: 42, Ref: monster.MonsterRef{MaxHP: 100}, CurrentHP: 100, Spawn: monster.SpawnPoint{RegionID: 0x6060, X: 10, Z: 10}}
	mover := monster.PendingMover{Pose: monster.Pose{RegionID: 0x6060, X: 10, Z: 10}}.Expand()
	d.instances.set(42, actor)
	d.movers.set(42, mover)
	d.byRegion[0x6060] = []uint32{42}
	d.instances.freeze(42)
	if len(d.instances.cold) != 1 {
		t.Fatal("fixture did not archive")
	}
	archive := d.instances.archive
	d.instances.archive = nil // Any accidental full actor read now fails immediately.
	defer func() { d.instances.archive = archive }()
	if got, ok := s.Mover("world", 42); !ok || got != mover {
		t.Fatal("mover changed")
	}
	if lease, ok := s.ObjectPopulation("world", 42); !ok || lease != d.lease {
		t.Fatal("lease changed")
	}
	viewer := worldgeom.RegionXZ{RegionID: 0x6060, X: 10, Z: 10}
	gids, rows := s.populationInterestDelta("world", d.lease, viewer, 0, map[uint32]bool{42: true})
	if len(gids) != 1 || gids[0] != 42 || len(rows) != 0 {
		t.Fatal("known actor copied or lost")
	}
	viewer.X = 1500
	if got := s.PopulationInterestInstances("world", d.lease, viewer, 0); len(got) != 0 {
		t.Fatal("out-of-interest actor returned")
	}
	d.instances.archive = archive
	viewer.X = 10
	gids, rows = s.populationInterestDelta("world", d.lease, viewer, 0, nil)
	if len(gids) != 1 || rows[42] != actor {
		t.Fatal("new viewer did not receive exact snapshot")
	}
	if len(d.instances.hot) != 0 {
		t.Fatal("read woke actor")
	}
	if got, ok := s.GetInPopulation("world", d.lease, 42); !ok || got != actor {
		t.Fatal("population read changed actor")
	}
	if got, ok := s.GetInWorld("world", d.lease.ID, 42); !ok || got != actor {
		t.Fatal("world read changed actor")
	}
	if len(d.instances.hot) != 0 {
		t.Fatal("read-only scoped lookup removed actor from archive")
	}
}

/*
================
BenchmarkMonsterVisibilityKnown1000
================
*/
func BenchmarkMonsterVisibilityKnown1000(b *testing.B) {
	s, d := queueBenchmarkState(1000)
	known := make(map[uint32]bool, 1000)
	d.byRegion = map[uint16][]uint32{}
	for gid := uint32(1); gid <= 1000; gid++ {
		d.movers.set(gid, monster.PendingMover{Pose: monster.Pose{RegionID: 0x6060, X: 10, Z: 10}}.Expand())
		known[gid] = true
	}
	for gid := uint32(1); gid <= 1000; gid++ {
		d.byRegion[0x6060] = append(d.byRegion[0x6060], gid)
	}
	viewer := worldgeom.RegionXZ{RegionID: 0x6060, X: 10, Z: 10}
	for _, delta := range []bool{false, true} {
		name := "snapshots"
		if delta {
			name = "delta"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if delta {
					gids, rows := s.populationInterestDelta("benchmark", d.lease, viewer, 0, known)
					if len(gids) != 1000 || len(rows) != 0 {
						b.Fatal("wrong delta")
					}
				} else {
					if len(s.PopulationInterestInstances("benchmark", d.lease, viewer, 0)) != 1000 {
						b.Fatal("lost actor")
					}
				}
			}
		})
	}
}
