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
TestPlayerKillOfARiddenTransportDropsWhereItDied

The reported lane: a thief player kills the transport its trader rides.
================
*/
func TestPlayerKillOfARiddenTransportDropsWhereItDied(t *testing.T) {
	rt, _, attacker, owner := newPvpPair(t)
	equipStrikeTestCOS(t, rt, owner)
	died := rideAwayFromAnchor(t, rt, owner)
	gid := owner.ActiveCOS.GID
	out := rt.HandleTargetInteract(testDivision, attacker, wire.BasicAttackEngage{TargetGid: gid}.Encode())
	if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok {
		t.Fatalf("no cast result: %+v (%s)", out.Frames, out.DiagnosticRefusal)
	}
	if owner.ActiveCOS != nil {
		t.Fatal("the killed transport kept its record")
	}
	assertCargoAt(t, rt.Ground.All(testDivision), died)
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
