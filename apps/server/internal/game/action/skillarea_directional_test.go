/*
===========================================================================

skillarea_directional_test.go - native vector rounding and directional victims

Pin machine-executed vector bits and the width boundary before exercising
the same selector through gameplay admission and target limits.

===========================================================================
*/

package action

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestInDirectionalShape

58AF60 uses rounded float stores, not ideal geometry. Executing the native
math block at 58B07D admits (50,0,10) at width 10 with zero body radii.
================
*/
func TestInDirectionalShape(t *testing.T) {
	dir := vec3{100, 0, 0}
	for _, tc := range []struct {
		name              string
		rel               vec3
		caster, candidate int32
		want              bool
	}{
		{"on the line", vec3{50, 0, 0}, 0, 0, true},
		{"inside the width", vec3{50, 0, 9}, 0, 0, true},
		{"native rounded width boundary is inside", vec3{50, 0, 10}, 0, 0, true},
		{"past rounded width boundary", vec3{50, 0, 10.0001}, 0, 0, false},
		{"at the reach", vec3{100, 0, 0}, 0, 0, true},
		{"past the reach", vec3{101, 0, 0}, 0, 0, false},
		{"radii extend the reach", vec3{110, 0, 0}, 4, 6, true},
		{"radius widens the line", vec3{50, 0, 15}, 0, 6, true},
		{"height is ignored", vec3{50, 1000, 0}, 0, 0, true},
		{"behind the caster still counts", vec3{-50, 0, 5}, 0, 0, true},
		{"on the caster", vec3{0, 0, 0}, 0, 0, true},
	} {
		if got := inDirectionalShape(tc.rel, dir, tc.caster, tc.candidate, 10); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

/*
================
TestAreaShapeThreeSelectsAlongTheLine

The primary comes first, followed by admitted monsters up to MaxTargets.
A monster outside the line remains excluded through the runtime path.
================
*/
func TestAreaShapeThreeSelectsAlongTheLine(t *testing.T) {
	rt, clock, c, primary := newCombatTestRuntime(t, 100000)
	origin := simulation.SeedWorldState(c).Spawn
	origin.X = primary.Spawn.X - 50
	origin.Y, origin.Z = primary.Spawn.Y, primary.Spawn.Z
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) { w.Spawn = origin })

	var nests []monster.NestRow
	for _, offset := range [][2]float64{{0, 0}, {20, 0}, {-20, 60}, {40, 0}} {
		spawn := primary.Spawn
		spawn.X += offset[0]
		spawn.Z += offset[1]
		nests = append(nests, monster.NestRow{SpawnPoint: spawn, RetailEvidence: true, MaxCount: 1})
	}
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{primary.Ref.RefObjID: primary.Ref}, nests))
	rt.Monsters.SetTimeSource(clock.Now)
	rt.Monsters.StartDivision(testDivision)
	rt.Monsters.AdvancePopulation(rt.Monsters.CurrentTimeMillis())
	targets := rt.Monsters.InstancesInRegions(testDivision, []uint16{primary.Spawn.RegionID})

	area := enterworld.SkillOffensiveArea{Shape: 3, Radius: 10, MaxTargets: 5, Select: 24}
	got := areaTestVictims(rt, c, targets[0], area, 150, clock.Now().UnixMilli())
	if len(got) != 3 || got[0].Gid != targets[0].Gid || got[1].Gid != targets[1].Gid || got[2].Gid != targets[3].Gid {
		t.Fatalf("shape 3 picked %+v", got)
	}
	area.MaxTargets = 2
	if got := areaTestVictims(rt, c, targets[0], area, 150, clock.Now().UnixMilli()); len(got) != 2 {
		t.Fatalf("MaxTargets 2 picked %d", len(got))
	}
}

/*
================
TestNativeVectorNormalizationBits

Captured by executing SR_GameServer 4328C0 through its original sqrt helper
in Unicorn, with default x87 control 037F and a caller return sentinel.
These values distinguish reciprocal multiplication from component division.
================
*/
func TestNativeVectorNormalizationBits(t *testing.T) {
	for _, row := range []struct {
		input vec3
		bits  [3]uint32
	}{
		{vec3{}, [3]uint32{}},
		{vec3{50, 0, 10}, [3]uint32{1065027414, 0, 1044959915}},
		{vec3{1, 2, 3}, [3]uint32{1049155191, 1057543799, 1062027698}},
		{vec3{-17, 23, 11}, [3]uint32{3205367212, 1061168586, 1052232474}},
		{vec3{300, 40, 70}, [3]uint32{1064778331, 1040440027, 1046920959}},
	} {
		got := row.input.normalized()
		bits := [3]uint32{math.Float32bits(got.x), math.Float32bits(got.y), math.Float32bits(got.z)}
		if bits != row.bits {
			t.Fatalf("normalize %+v: bits %v, native %v", row.input, bits, row.bits)
		}
	}
}
