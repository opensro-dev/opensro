/*
===========================================================================

temple_sanctum_test.go - the sanctum gates open, chain and close on the
shard's schedule

===========================================================================
*/

package action

import (
	"bytes"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/pharaohtemple"
	"opensro.online/server/internal/game/world/simulation"
)

var (
	sanctumTestZone = time.FixedZone("shard", 0)
	// The Haroeris & Seth window that starts at 03:00 opens its gates at
	// 03:30 and ends at 05:00.
	sanctumTestWindow = time.Date(2026, 11, 24, 3, 0, 0, 0, sanctumTestZone)
	sanctumTestOpen   = sanctumTestWindow.Add(pharaohtemple.OpenAfter)
	sanctumTestEnd    = sanctumTestWindow.Add(2 * time.Hour)
	// A point inside the Temple, away from every gate.
	sanctumTestTemple = simulation.Spawn{RegionID: pharaohtemple.TempleRegionID, X: 100, Y: 41, Z: 100}
)

/*
================
sanctumFixture

A runtime with the sanctums on, both teleporter buildings in the
catalogue, the clock just before the Haroeris & Seth open and one
character standing in the Temple.
================
*/
func sanctumFixture(t *testing.T) (*Runtime, *fakeClock, *rocTestMonsters, *enterworld.Character) {
	t.Helper()
	c := instanceWorldTestCharacter(61, "Pilgrim")
	rt, clock := newInstanceWorldRuntime(t, c)
	rt.portals = &portalCatalog{teleporters: map[string]*questGateBuilding{
		pharaohtemple.TempleTeleporter: {code: pharaohtemple.TempleTeleporter, ref: 27760, tid: 0x130, height: 90, radius: 40},
		pharaohtemple.SethTeleporter:   {code: pharaohtemple.SethTeleporter, ref: 27759, tid: 0x130, height: 90, radius: 40},
	}}
	admitInstanceWorldCharacter(t, rt, c, 1, sanctumTestTemple)
	rt.ConfigureTempleSanctums(true)
	monsters := &rocTestMonsters{spawned: map[uint32]simulation.QuestMonsterSpawn{}, alive: map[uint32]bool{}}
	rt.sanctums.loc = sanctumTestZone
	rt.sanctums.spawn = monsters.spawn
	rt.sanctums.alive = func(_ string, gid uint32) bool { return monsters.alive[gid] }
	clock.now = sanctumTestOpen.Add(-time.Second)
	return rt, clock, monsters, c
}

/*
================
sanctumGates

The Haroeris & Seth pair's gates standing in the division's open world,
by building code. The windows overlap (Anubis & Isis runs to 03:30), so
a gate counts only when it stands on one of this pair's gate points.
================
*/
func sanctumGates(rt *Runtime, division string) map[string][]simulation.NpcDef {
	points := map[simulation.Spawn]bool{}
	for _, sanctum := range pharaohtemple.Sanctums() {
		if sanctum.Schedule == pharaohtemple.ScheduleHaroerisSeth {
			points[sanctumSpawn(sanctum.Enter)] = true
			points[sanctumSpawn(sanctum.Leave)] = true
		}
	}
	out := map[string][]simulation.NpcDef{}
	for _, def := range rt.QuestGateNpcs(division, domain.DefaultWorldInstance) {
		if points[def.Spawn] {
			out[def.Codename] = append(out[def.Codename], def)
		}
	}
	return out
}

/*
================
TestOverlappingWindowsKeepTheirOwnGates

At 03:29 the Anubis & Isis pair (01:30-03:30) still stands its four
gates while the Haroeris & Seth pair waits for its open; at 03:30 the
first pair closes as the second opens.
================
*/
func TestOverlappingWindowsKeepTheirOwnGates(t *testing.T) {
	rt, clock, _, _ := sanctumFixture(t)
	sanctumTick(rt, clock)
	if all := rt.QuestGateNpcs(testDivision, domain.DefaultWorldInstance); len(all) != 4 {
		t.Fatalf("%d gates at 03:29, want Anubis's and Isis's four", len(all))
	}
	clock.now = sanctumTestOpen
	sanctumTick(rt, clock)
	if all := rt.QuestGateNpcs(testDivision, domain.DefaultWorldInstance); len(all) != 2 || len(sanctumGates(rt, testDivision)) != 1 {
		t.Fatalf("%d gates at 03:30, want Haroeris's two alone", len(all))
	}
}

/*
================
sanctumTick
================
*/
func sanctumTick(rt *Runtime, clock *fakeClock) {
	rt.advanceTempleSanctums(clock.now.UnixMilli())
}

/*
================
TestSanctumGatesOpenHalfAnHourIntoTheWindow

Nothing stands during the notice half hour. At the open, Haroeris's
enter gate stands in the Temple and his leave gate in his sanctum,
Haroeris spawns once at the wave's point for the rest of the window, and
no other pair's gate stands. Another division sees none of it.
================
*/
func TestSanctumGatesOpenHalfAnHourIntoTheWindow(t *testing.T) {
	rt, clock, monsters, _ := sanctumFixture(t)
	sanctumTick(rt, clock)
	if gates := sanctumGates(rt, testDivision); len(gates) != 0 {
		t.Fatalf("gates before the open: %v", gates)
	}
	clock.now = sanctumTestOpen
	sanctumTick(rt, clock)
	sanctumTick(rt, clock)
	gates := sanctumGates(rt, testDivision)
	temple := gates[pharaohtemple.TempleTeleporter]
	if len(temple) != 2 || len(gates[pharaohtemple.SethTeleporter]) != 0 {
		t.Fatalf("at the open: %d temple gates, %d Seth gates, want 2 and 0", len(temple), len(gates[pharaohtemple.SethTeleporter]))
	}
	regions := map[uint16]bool{}
	for _, def := range temple {
		regions[def.Spawn.RegionID] = true
	}
	if !regions[pharaohtemple.TempleRegionID] || !regions[0x800E] {
		t.Fatalf("gates stand in %v, want the Temple and Haroeris's sanctum", regions)
	}
	if len(monsters.spawned) != 1 {
		t.Fatalf("%d spawns, want Haroeris once", len(monsters.spawned))
	}
	haroeris := monsters.spawned[monsters.gidOf(haroerisCodename)]
	if haroeris.Position != haroerisSpawn || !haroeris.FixedPosition {
		t.Fatalf("Haroeris at %+v (fixed %v), want %+v", haroeris.Position, haroeris.FixedPosition, haroerisSpawn)
	}
	if want := sanctumTestEnd.Sub(sanctumTestOpen).Milliseconds(); haroeris.LifetimeMs != want {
		t.Fatalf("Haroeris lives %d ms, want the window's rest %d", haroeris.LifetimeMs, want)
	}
	if other := rt.QuestGateNpcs("another-division", domain.DefaultWorldInstance); len(other) != 0 {
		t.Fatalf("another division sees %v", other)
	}
}

/*
================
TestSethOpensWhenHaroerisDies

Seth's enter gate (his own teleporter, in Haroeris's sanctum) and leave
gate stand on the tick after Haroeris dies, once.
================
*/
func TestSethOpensWhenHaroerisDies(t *testing.T) {
	rt, clock, monsters, _ := sanctumFixture(t)
	clock.now = sanctumTestOpen
	sanctumTick(rt, clock)
	clock.now = clock.now.Add(time.Minute)
	sanctumTick(rt, clock)
	if seth := sanctumGates(rt, testDivision)[pharaohtemple.SethTeleporter]; len(seth) != 0 {
		t.Fatal("Seth's gate stands while Haroeris lives")
	}
	monsters.alive[monsters.gidOf(haroerisCodename)] = false
	sanctumTick(rt, clock)
	sanctumTick(rt, clock)
	gates := sanctumGates(rt, testDivision)
	seth := gates[pharaohtemple.SethTeleporter]
	if len(seth) != 1 || seth[0].Spawn.RegionID != 0x800E || seth[0].Spawn.X != 9384 {
		t.Fatalf("Seth gates %+v, want one at (9384, 102, 23) in 0x800E", seth)
	}
	if len(gates[pharaohtemple.TempleTeleporter]) != 3 {
		t.Fatalf("%d temple gates, want Haroeris's two and Seth's leave gate", len(gates[pharaohtemple.TempleTeleporter]))
	}
}

/*
================
TestSanctumGateTravelsToItsTarget

The Temple-side gate moves the selecting character into Haroeris's
sanctum, to the enter target the group config names.
================
*/
func TestSanctumGateTravelsToItsTarget(t *testing.T) {
	rt, clock, _, c := sanctumFixture(t)
	clock.now = sanctumTestOpen
	sanctumTick(rt, clock)
	var gid uint32
	for _, def := range sanctumGates(rt, testDivision)[pharaohtemple.TempleTeleporter] {
		if def.Spawn.RegionID == pharaohtemple.TempleRegionID {
			gid = def.ObjectID
		}
	}
	key := simulation.WorldKey(testDivision, c.Name)
	rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) {
		w.Spawn = simulation.Spawn{RegionID: pharaohtemple.TempleRegionID, X: 7770, Y: 112, Z: 26}
	})
	rt.Selected.Set(testDivision, c.Name, gid)
	out := questGateTravel(rt, c, gid, 0)
	if len(out.Frames) > 0 && bytes.Equal(out.Frames[0].Payload, portalFailure(errCodeGateMoveFailed).Frames[0].Payload) {
		t.Fatal("the gate refused the move")
	}
	if pose := rt.liveSpawn(key, c, 0); pose.RegionID != 0x800E || pose.X != 8240 || pose.Z != 37 {
		t.Fatalf("arrived at %+v, want (8240, 102, 37) in 0x800E", pose)
	}
}

/*
================
TestSanctumCloseTakesTheGatesDownAndEmptiesTheSanctums

At the window's end every gate of the pair comes down and a character
still in Seth's sanctum leaves to its leave target in the Temple; one
standing in the Temple stays where it is.
================
*/
func TestSanctumCloseTakesTheGatesDownAndEmptiesTheSanctums(t *testing.T) {
	rt, clock, monsters, c := sanctumFixture(t)
	clock.now = sanctumTestOpen
	sanctumTick(rt, clock)
	monsters.alive[monsters.gidOf(haroerisCodename)] = false
	sanctumTick(rt, clock)
	key := simulation.WorldKey(testDivision, c.Name)
	rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) {
		w.Spawn = simulation.Spawn{RegionID: 0x800F, X: 4, Y: -143, Z: -815}
	})
	clock.now = sanctumTestEnd
	sanctumTick(rt, clock)
	if gates := sanctumGates(rt, testDivision); len(gates) != 0 {
		t.Fatalf("gates after the close: %v", gates)
	}
	if pose := rt.liveSpawn(key, c, 0); pose.RegionID != pharaohtemple.TempleRegionID || pose.X != 7696 || pose.Z != 26 {
		t.Fatalf("after the close at %+v, want Seth's leave target (7696, 112, 26) in the Temple", pose)
	}
	rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) {
		w.Spawn = sanctumTestTemple
	})
	clock.now = sanctumTestEnd.Add(time.Minute)
	sanctumTick(rt, clock)
	if pose := rt.liveSpawn(key, c, 0); pose != sanctumTestTemple {
		t.Fatalf("a character in the Temple moved to %+v", pose)
	}
}

/*
================
TestSanctumsOffStandNothing

With the temple off no gate ever stands and nothing spawns.
================
*/
func TestSanctumsOffStandNothing(t *testing.T) {
	rt, clock, monsters, _ := sanctumFixture(t)
	rt.ConfigureTempleSanctums(false)
	clock.now = sanctumTestOpen
	sanctumTick(rt, clock)
	if gates := sanctumGates(rt, testDivision); len(gates) != 0 || len(monsters.spawned) != 0 {
		t.Fatalf("temple off: gates %v, %d spawns", gates, len(monsters.spawned))
	}
}
