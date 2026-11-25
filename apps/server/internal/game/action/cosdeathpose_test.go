/*
===========================================================================

cosdeathpose_test.go - a ridden transport's cargo lands where it died

While its trader rides it, a transport moves in the rider's world; its
own parked pose (the summon spot, or its last dismount) goes stale. A
death that unmounts the rider must hand the vehicle the rider's live
pose before the cargo scatters, or the goods appear back at the
specialty shop (the "Trader goods" report: a thief player's kill
dropped them in front of the shop, or where an earlier death had
re-summoned the vehicle).

===========================================================================
*/
package action

import (
	"math"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// How far a scattered bag may land from the vehicle (the drop scatter
// is a few units), and how far the stale anchor sits from the rider:
// far enough apart that no scatter can mistake one for the other.
const (
	cargoScatterSlack = 30.0
	staleAnchorOffset = 400.0
)

/*
================
rideAwayFromAnchor

The trader's run as a player plays it: the transport is parked at the
"shop" staleAnchorOffset units east of where owner stands, owner walks
to it, mounts it through the ride packet (HandleCosRide), and rides
back. Returns the pose ridden to, where the goods must land.
================
*/
func rideAwayFromAnchor(t *testing.T, rt *Runtime, owner *enterworld.Character) simulation.Spawn {
	t.Helper()
	pet := owner.ActiveCOS
	potion := testItems()["ITEM_ETC_HP_POTION_01"]
	pet.Container = &domain.COSContainer{Capacity: 4, Rows: []enterworld.InventoryRow{{
		Slot: 0, RefObjID: potion.RefObjID, Codename: potion.Codename, TypeFlags: potion.TypeFlags(), StackCount: 7}}}
	pet.CurrentHP = 1
	rt.BindPetSession(testDivision, owner, 1)
	key := simulation.WorldKey(testDivision, owner.Name)
	field := rt.liveSpawn(key, owner, rt.Now().UnixMilli())
	shop := field
	shop.X += staleAnchorOffset
	rt.rememberTransportCOS(testDivision, owner, shop)
	moveTo := func(pose simulation.Spawn) {
		rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(owner) },
			func(w *simulation.WorldState) { w.Spawn = pose })
	}
	moveTo(shop)
	// The combat fixtures start in battle; a trader leaving the shop is
	// not, and the ride door refuses a rider in battle (code 11).
	owner.BattleUntilMs = 0
	ride := rt.HandleCosRide(testDivision, owner, wire.NewWriter(5).U8(1).U32(pet.GID).Payload())
	if !pet.Mounted {
		t.Fatalf("the trader could not mount the parked transport: %+v", ride.Frames)
	}
	moveTo(field)
	if at := rt.cosLiveSpawn(testDivision, owner, rt.Now().UnixMilli()); simulation.WorldDistance2D(at, field) > 1 {
		t.Fatalf("the ridden transport reads %+v, want the rider's %+v", at, field)
	}
	return field
}

/*
================
assertCargoAt

The cargo's single bag landed within the scatter of want.
================
*/
func assertCargoAt(t *testing.T, ground []grounditem.Item, want simulation.Spawn) {
	t.Helper()
	if len(ground) != 1 || ground[0].StackCount != 7 {
		t.Fatalf("cargo %+v, want the one 7-potion bag", ground)
	}
	at := ground[0].Position
	distance := math.Hypot(float64(at.X)-want.X, float64(at.Z)-want.Z)
	if at.RegionID != want.RegionID || distance > cargoScatterSlack {
		t.Fatalf("cargo landed at %+v, %.0f from the dead vehicle at %+v (the stale anchor is %.0f east)",
			at, distance, want, staleAnchorOffset)
	}
}

/*
================
assertVehicleUnmounted

The bag alone cannot pin the ridden death's pose hand-off: while
Mounted still reads true the settle falls back to the rider, so a bag
at the rider's pose proves nothing about the vehicle's own mover.
What proves the hand-off ran is its other half: the commit's unmount
(cosabnormal.go) is the one call that parks the mover at the rider's
pose and clears Mounted, in that order, before the settle reads the
pose back. Mounted surviving the kill is the park having never run.
(The mover itself cannot be read after the kill to check the park:
the death publication retires it at once.)
================
*/
func assertVehicleUnmounted(t *testing.T, pet *enterworld.CharacterCOS) {
	t.Helper()
	if pet.Mounted {
		t.Fatal("the killed vehicle still reads mounted: the commit's unmount never parked it at the rider's pose")
	}
}

/*
================
TestPlayerKillOfARiddenTransportDropsWhereItDied

The reported lane: a thief player kills the transport its trader rides.
================
*/
func TestPlayerKillOfARiddenTransportDropsWhereItDied(t *testing.T) {
	rt, _, attacker, owner := newPvpPair(t)
	equipStrikeTestCOS(t, rt, owner)
	died := rideAwayFromAnchor(t, rt, owner)
	pet := owner.ActiveCOS
	gid := pet.GID
	out := rt.HandleTargetInteract(testDivision, attacker, wire.BasicAttackEngage{TargetGid: gid}.Encode())
	if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok {
		t.Fatalf("no cast result: %+v (%s)", out.Frames, out.DiagnosticRefusal)
	}
	if owner.ActiveCOS != nil {
		t.Fatal("the killed transport kept its record")
	}
	assertCargoAt(t, rt.Ground.All(testDivision), died)
	assertVehicleUnmounted(t, pet)
}

/*
================
TestPlayerKillOfAFollowingTransportDropsWhereItDied

The unridden half of the report: the transport is parked at the shop,
the trader walks far with the follow rule on so it walks after him,
then walks on alone. A thief player kills the transport where it
stands - more than the scatter slack from both the parked summon spot
and the trader, so "dropped at the owner" and "dropped at the
transport" give different answers. The goods land at the transport.
================
*/
func TestPlayerKillOfAFollowingTransportDropsWhereItDied(t *testing.T) {
	rt, clock, attacker, owner := newPvpPair(t)
	equipStrikeTestCOS(t, rt, owner)
	pet := owner.ActiveCOS
	potion := testItems()["ITEM_ETC_HP_POTION_01"]
	pet.Container = &domain.COSContainer{Capacity: 4, Rows: []enterworld.InventoryRow{{
		Slot: 0, RefObjID: potion.RefObjID, Codename: potion.Codename, TypeFlags: potion.TypeFlags(), StackCount: 7}}}
	rt.BindPetSession(testDivision, owner, 1)
	rt.TransportFollow = true
	// An open field: every step lands where it aims (as dismountedTransportApart).
	rt.ConstrainMovement = func(_ string, _, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) { return to, nil }
	shop := rt.liveSpawn(simulation.WorldKey(testDivision, owner.Name), owner, clock.NowMs())
	rt.rememberTransportCOS(testDivision, owner, shop)
	field := shop
	field.X += staleAnchorOffset
	moveOwner := func(pose simulation.Spawn) {
		rt.Worlds.Update(simulation.WorldKey(testDivision, owner.Name), func() simulation.WorldState { return simulation.SeedWorldState(owner) },
			func(w *simulation.WorldState) { w.Spawn = pose; w.SpawnSet = true })
	}
	moveOwner(field)
	for i := 0; i < 60; i++ {
		clock.Advance(500_000_000)
		rt.advancePets(clock.NowMs())
	}
	// The trader walks on alone; no pet tick runs again, so the
	// transport stands where the follow left it.
	ownerFar := field
	ownerFar.X += 200
	moveOwner(ownerFar)
	died := rt.companionLiveSpawn(testDivision, owner, pet, clock.NowMs())
	if simulation.WorldDistance2D(died, shop) < 100 || simulation.WorldDistance2D(died, ownerFar) < 100 {
		t.Fatalf("fixture: the transport is not apart from both anchors: at %+v, shop %+v, trader %+v", died, shop, ownerFar)
	}
	// The thief stands at the transport, not at the trader.
	rt.Worlds.Update(simulation.WorldKey(testDivision, attacker.Name), func() simulation.WorldState { return simulation.SeedWorldState(attacker) },
		func(w *simulation.WorldState) { spot := died; spot.X += 3; w.Spawn = spot; w.SpawnSet = true })
	pet.CurrentHP = 1
	rt.HandleTargetInteract(testDivision, attacker, wire.BasicAttackEngage{TargetGid: pet.GID}.Encode())
	for i := 0; i < 3 && owner.ActiveCOS != nil; i++ {
		clock.Advance(100_000_000)
		rt.advanceProjectileCasts(clock.NowMs())
		rt.TickHook()(clock.NowMs())
	}
	if owner.ActiveCOS != nil {
		t.Fatal("the following transport survived the thief's strike")
	}
	assertCargoAt(t, rt.Ground.All(testDivision), died)
}

/*
================
TestPlayerKillOfAResummonedTransportDropsWhereItDied

The report's third case: a first kill drops the goods at A, the trader
summons a fresh transport at A, loads the recovered goods and rides to
C, and a thief player kills that one too. The second drop lands at C,
not back at the earlier death point A.
================
*/
func TestPlayerKillOfAResummonedTransportDropsWhereItDied(t *testing.T) {
	rt, clock, attacker, owner := newPvpPair(t)
	equipStrikeTestCOS(t, rt, owner)
	died1 := rideAwayFromAnchor(t, rt, owner)
	gid := owner.ActiveCOS.GID
	kill := func(pet *enterworld.CharacterCOS) {
		t.Helper()
		rt.HandleTargetInteract(testDivision, attacker, wire.BasicAttackEngage{TargetGid: gid}.Encode())
		for i := 0; i < 40 && owner.ActiveCOS != nil; i++ {
			clock.Advance(100_000_000)
			rt.advanceProjectileCasts(clock.NowMs())
			rt.TickHook()(clock.NowMs())
		}
		if owner.ActiveCOS != nil {
			t.Fatal("the transport survived the thief's strike")
		}
		assertVehicleUnmounted(t, pet)
	}
	kill(owner.ActiveCOS)
	assertCargoAt(t, rt.Ground.All(testDivision), died1)
	// The trader summons the next transport where he stands (at A) -
	// the mover-relevant tail of the item-use summon door - and the
	// recovered goods go aboard.
	pet := &enterworld.CharacterCOS{GID: gid, RefObjID: 9, Codename: "PET", Level: 1,
		CurrentHP: 1, CurrentMP: 600, StateFlags: 1, Summoned: true}
	potion := testItems()["ITEM_ETC_HP_POTION_01"]
	pet.Container = &domain.COSContainer{Capacity: 4, Rows: []enterworld.InventoryRow{{
		Slot: 0, RefObjID: potion.RefObjID, Codename: potion.Codename, TypeFlags: potion.TypeFlags(), StackCount: 7}}}
	owner.ActiveCOS = pet
	rideNewVehicle(pet)
	rt.rememberTransportCOS(testDivision, owner, rt.liveSpawn(simulation.WorldKey(testDivision, owner.Name), owner, clock.NowMs()))
	// The recovered bag leaves the ground (the pickup), the ride to C begins.
	for _, item := range rt.Ground.All(testDivision) {
		rt.Ground.Remove(testDivision, item.Gid)
	}
	died2 := died1
	died2.X += 300
	moveOwner := func(pose simulation.Spawn) {
		rt.Worlds.Update(simulation.WorldKey(testDivision, owner.Name), func() simulation.WorldState { return simulation.SeedWorldState(owner) },
			func(w *simulation.WorldState) { w.Spawn = pose; w.SpawnSet = true })
	}
	moveOwner(died2)
	rt.Worlds.Update(simulation.WorldKey(testDivision, attacker.Name), func() simulation.WorldState { return simulation.SeedWorldState(attacker) },
		func(w *simulation.WorldState) { spot := died2; spot.X += 3; w.Spawn = spot; w.SpawnSet = true })
	kill(pet)
	assertCargoAt(t, rt.Ground.All(testDivision), died2)
}

/*
================
TestMonsterKillOfARiddenTransportDropsWhereItDied

The same death dealt by a monster thief: one settlement, one pose.
================
*/
func TestMonsterKillOfARiddenTransportDropsWhereItDied(t *testing.T) {
	rt, _, c, instance := newCombatTestRuntime(t, 100)
	instance.Ref.DefaultSkillIDs[0] = 2
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	skills := rt.deps.SkillData().(staticSkillSource)
	strike := skills[2]
	strike.Attack.Min, strike.Attack.Max, strike.Attack.Percent = 1000, 1000, 100
	skills[2] = strike
	equipCombatTestPet(t, rt, c, cosBandTransport)
	died := rideAwayFromAnchor(t, rt, c)
	result := rt.MonsterBasicAttack(testDivision, instance, c.ActiveCOS.GID, 2, rt.Now().UnixMilli())
	if !result.Accepted {
		t.Fatalf("the strike on the transport was refused: %+v", result)
	}
	if c.ActiveCOS != nil {
		t.Fatal("the killed transport kept its record")
	}
	assertCargoAt(t, rt.Ground.All(testDivision), died)
}
