/*
===========================================================================

gmmonster_test.go - GM /LOADMONSTER population, clamps and type resolution

===========================================================================
*/
package simulation

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
gmMonsterState
================
*/
func gmMonsterState(t *testing.T, monsterType uint8) (*MonsterState, instance.Lease) {
	t.Helper()
	state := NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{
		1: {RefObjID: 1, Codename: "MOB_CH_MANGNYANG", TidWord: 0xc6, MaxHP: 100, WalkSpeed: 8, RunSpeed: 20, MonsterType: monsterType},
	}, nil))
	state.SetRandomSource(func() float64 { return 0 })
	state.StartDivision("gm")
	lease, exists := state.PopulationLease("gm", instance.Pack(1, 1))
	if !exists {
		t.Fatal("ordinary population absent")
	}
	return state, lease
}

/*
================
TestGMMonstersSpawnAtTheGMWithNativeClamps

520A40 creates count instances on the GM's point (count clamped 1..250),
with no respawning nest, and refuses an unknown reference.
================
*/
func TestGMMonstersSpawnAtTheGMWithNativeClamps(t *testing.T) {
	state, lease := gmMonsterState(t, 0)
	at := Spawn{RegionID: 0x655e, X: 253, Y: 85, Z: 449}
	request := GMMonsterSpawn{Division: "gm", Population: lease, RefObjID: 1, Count: 5, Position: at, NowMs: 1000}
	if created := state.SpawnGMMonsters(request); created != 5 {
		t.Fatalf("created %d, want 5", created)
	}
	actors := state.MaterializedInstances("gm")
	if len(actors) != 5 {
		t.Fatalf("population %d, want 5", len(actors))
	}
	for _, actor := range actors {
		if !actor.NestDetached || actor.CurrentHP != 100 || actor.Spawn.X != 253 || actor.Spawn.Z != 449 {
			t.Fatalf("GM monster left the GM point, gained a nest or lost its HP: %+v", actor)
		}
	}
	zero := request
	zero.Count = 0
	if created := state.SpawnGMMonsters(zero); created != 1 {
		t.Fatalf("count 0 created %d, want the native minimum 1", created)
	}
	many := request
	many.Count = 255
	if created := state.SpawnGMMonsters(many); created != gmMonsterMaxCount {
		t.Fatalf("count 255 created %d, want the native maximum %d", created, gmMonsterMaxCount)
	}
	unknown := request
	unknown.RefObjID = 2
	if state.SpawnGMMonsters(unknown) != 0 {
		t.Fatal("an unknown reference spawned")
	}
	stale := request
	stale.Population.Generation++
	if state.SpawnGMMonsters(stale) != 0 {
		t.Fatal("a stale population generation spawned")
	}
	if !state.Defeat("gm", actors[0].Gid, time.UnixMilli(1001)) {
		t.Fatal("a GM monster could not die through the ordinary population owner")
	}
}

/*
================
TestGMMonstersFaceAroundTheFullTurn

The heading is a radian draw converted to the wire word, so random draws
spread over the whole circle rather than collapsing near zero.
================
*/
func TestGMMonstersFaceAroundTheFullTurn(t *testing.T) {
	state, lease := gmMonsterState(t, 0)
	draws := []float64{0, 0.25, 0.5, 0.75}
	next := 0
	state.SetRandomSource(func() float64 {
		value := draws[next%len(draws)]
		next++
		return value
	})
	request := GMMonsterSpawn{Division: "gm", Population: lease, RefObjID: 1, Count: 4,
		Position: Spawn{RegionID: 0x655e, X: 253, Y: 85, Z: 449}, NowMs: 1000}
	if state.SpawnGMMonsters(request) != 4 {
		t.Fatal("load refused")
	}
	largest := uint16(0)
	for _, actor := range state.MaterializedInstances("gm") {
		if actor.SpawnHeading > largest {
			largest = actor.SpawnHeading
		}
	}
	if largest < 0x8000 {
		t.Fatalf("headings stay below %#x; radians were not converted to the wire word", largest)
	}
}

/*
================
TestGMSpawnRarityMatchesNative520D90
================
*/
func TestGMSpawnRarityMatchesNative520D90(t *testing.T) {
	cases := []struct{ requested, own, want uint8 }{
		{0, 0, 0}, {1, 0, 1}, {4, 0, 4}, {7, 0, 7},
		{2, 0, 0}, {5, 1, 1}, {0x0f, 4, 4},
		{1, 3, 3}, {4, 8, 8}, {0, 3, 3},
		{0x11, 0, 1}, {4, 0x13, 3},
	}
	for _, c := range cases {
		if got := GMSpawnRarity(c.requested, c.own); got != c.want {
			t.Fatalf("GMSpawnRarity(%#x, %#x) = %#x, want %#x", c.requested, c.own, got, c.want)
		}
	}
	state, lease := gmMonsterState(t, 0)
	request := GMMonsterSpawn{Division: "gm", Population: lease, RefObjID: 1, Count: 1, Type: 4,
		Position: Spawn{RegionID: 0x655e, X: 253, Y: 85, Z: 449}, NowMs: 1000}
	if state.SpawnGMMonsters(request) != 1 {
		t.Fatal("giant request refused")
	}
	if actor := state.MaterializedInstances("gm")[0]; actor.Rarity() != 4 || actor.CurrentHP != actor.EffectiveMaxHP() {
		t.Fatalf("giant request produced rarity %d hp %d/%d", actor.Rarity(), actor.CurrentHP, actor.EffectiveMaxHP())
	}
}
