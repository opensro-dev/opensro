/*
===========================================================================

tomb_respawn_test.go - a tomb floor unique returns after its death

Regression coverage for #2729: a player reported the Qin-Shi tomb's
second-floor uniques never respawn. The floor's unique-grade family
(the Tomb General nests) is single-cap and respawn-enabled in the
shipped tomb population, in an ordinary hive. This test runs that
population: the unique spawns, dies, and is back once its rolled
respawn delay has passed. The delay itself is driven through the nest
runtime, never waited out in real time.

===========================================================================
*/

package simulation

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
)

// The second-floor region and its unique-grade codename, as the shipped
// tomb nests name them (qinshi_tomb_nests.tsv).
const (
	tombFloor2Region   = 32774
	tombFloor2Unique   = "MOB_TQ_TOMBGENERAL_CLON"
	tombRespawnBaseSec = 1_784_100_000
)

/*
================
TestTombFloor2UniqueRespawns

The B2 unique spawns from its shipped nest, a defeat empties the nest,
and the nest brings it back after its respawn delay.
================
*/
func TestTombFloor2UniqueRespawns(t *testing.T) {
	base := monster.TemplateFromParts(map[uint32]monster.MonsterRef{
		1933: {RefObjID: 1933, Codename: "MOB_CH_MANGNYANG", Level: 1, MaxHP: 59},
	}, nil)
	withTomb, err := base.WithQinShiTomb("")
	if err != nil {
		t.Fatalf("WithQinShiTomb: %v", err)
	}
	s := NewMonsterState(withTomb)
	base0 := time.UnixMilli(tombRespawnBaseSec * 1000)
	s.SetTimeSource(func() time.Time { return base0 })
	s.SetRandomSource(constantWord(16384))
	s.StartDivision("division")
	state := s.divs["division"]
	if state == nil {
		t.Fatal("no division state")
	}
	index := -1
	for i, nest := range withTomb.Nests {
		if nest.RegionID == tombFloor2Region && withTomb.Refs[nest.RefObjID].Codename == tombFloor2Unique {
			index = i
		}
	}
	if index < 0 {
		t.Fatal("the B2 unique's nest is not in the shipped tomb template")
	}
	if nest := withTomb.Nests[index]; nest.MaxCount != 1 || !nest.Respawn {
		t.Fatalf("the B2 unique's nest must be single-cap and respawn-enabled: %+v", nest)
	}
	// Bring the first spawn due; the authored initial delay is not the
	// subject of this test.
	n := state.nests[index]
	n.lastMs = 0
	n.intervalMs = 1
	s.AdvancePopulation(base0.Add(2 * time.Second).UnixMilli())
	var gid uint32
	count := 0
	for _, in := range s.InstancesInRegions("division", []uint16{tombFloor2Region}) {
		if in.Ref.Codename == tombFloor2Unique {
			count++
			gid = in.Gid
		}
	}
	if count != 1 || state.nests[index].live != 1 {
		t.Fatalf("want one live B2 unique, got %d (nest live %d)", count, state.nests[index].live)
	}
	if !s.Defeat("division", gid, base0.Add(3*time.Second)) {
		t.Fatal("the B2 unique's defeat was refused")
	}
	n = state.nests[index]
	if n.live != 0 {
		t.Fatalf("the defeated unique still holds its nest (live %d)", n.live)
	}
	if int64(n.intervalMs) < int64(withTomb.Nests[index].RespawnDelayMinSec)*1000 ||
		int64(n.intervalMs) > int64(withTomb.Nests[index].RespawnDelayMaxSec)*1000 {
		t.Fatalf("respawn delay %d ms outside the nest's authored range", n.intervalMs)
	}
	// Run the rolled delay out on the nest's own clock and tick once.
	n.lastMs = base0.Add(3*time.Second).UnixMilli() - int64(n.intervalMs) - 1
	s.AdvancePopulation(base0.Add(4 * time.Second).UnixMilli())
	count = 0
	for _, in := range s.InstancesInRegions("division", []uint16{tombFloor2Region}) {
		if in.Ref.Codename == tombFloor2Unique {
			count++
		}
	}
	if count != 1 || state.nests[index].live != 1 {
		t.Fatalf("the B2 unique did not return after its respawn delay (live %d)", state.nests[index].live)
	}
}
