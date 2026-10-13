/*
===========================================================================

petstuck_test.go - tests for petstuck.go: which COS the stuck rule warps

The native stuck rule (559010 -> 5599C0) relocates an attack, pickup or
fellow COS onto its owner after ten stalled stops, in FOLLOW and in PICKITEM
alike; BATTLE keeps fighting (55A250), and a pet that keeps reaching its
formation never accumulates the count.

===========================================================================
*/

package action

import (
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// stuckOwnerAt is an owner one sector from the stranded pet's region
// 0x62a8, inside 430CE0's adjacency, so only the stuck rule can warp.
var stuckOwnerAt = simulation.Spawn{RegionID: 0x62a9, X: 300, Z: 500}

/*
================
tickUntilWarp

Advance the pet in 100 ms ticks until it stands on owner; the tick index
of the warp, or -1.
================
*/
func tickUntilWarp(rt *Runtime, key petOwnerKey, state *petSession, owner simulation.Spawn, ticks int) ([]simulation.Frame, int) {
	var frames []simulation.Frame
	for i := 0; i < ticks; i++ {
		now := int64(2_000_000 + i*100)
		frames = append(frames, rt.advancePet(key, now)...)
		if state.follower == nil {
			continue
		}
		if at := state.follower.Position(now); at.RegionID == owner.RegionID && at.X == owner.X && at.Z == owner.Z {
			return frames, i
		}
	}
	return frames, -1
}

/*
================
TestWalledInPickupPetWarpsWhileFollowing

A pickup pet (band 4, 44B460) follows through the same FOLLOW state and
warps on its tenth stalled stop.
================
*/
func TestWalledInPickupPetWarpsWhileFollowing(t *testing.T) {
	rt, key, state := strandedPet(t, 4, stuckOwnerAt)
	if _, at := tickUntilWarp(rt, key, state, stuckOwnerAt, 50); at < 9 {
		t.Fatalf("a walled-in pickup pet warped on tick %d, want after its tenth stalled stop", at)
	}
}

/*
================
TestWalledInPickupPetWarpsWhileCollecting

PICKITEM uses the base stuck handler too (5599C0): a pickup pet walled away
from its item warps to its owner, and leaving PICKITEM answers the pending
pickup with a failure, as any state change does.
================
*/
func TestWalledInPickupPetWarpsWhileCollecting(t *testing.T) {
	rt, key, state := strandedPet(t, 4, stuckOwnerAt)
	c := state.character
	c.ActiveCOS.Container = &domain.COSContainer{Capacity: 2}
	pose := state.follower.Position(2_000_000)
	item := rt.Ground.Add(testDivision, grounditem.Item{GoldAmount: 50, Position: grounditem.Point{
		RegionID: pose.RegionID, X: float32(pose.X) + 100, Z: float32(pose.Z)}})
	body := wire.NewWriter(9).U32(key.gid).U8(wire.CosCommandPickupTag).U32(item.Gid).Payload()
	rt.HandleCosCommand(testDivision, c, body)
	if state.pickup == nil {
		t.Fatal("fixture: the pickup did not start an approach")
	}
	if _, at := tickUntilWarp(rt, key, state, stuckOwnerAt, 50); at < 9 {
		t.Fatalf("a walled-in collecting pet warped on tick %d, want after its tenth stalled stop", at)
	}
	if state.pickup != nil {
		t.Fatal("the warp left the pickup pending")
	}
}

/*
================
stallFollower

Drive a follower through n refused steps (each a stalled stop, 4A9430 event
8 counted by 559010) toward a far goal, starting at nowMs.
================
*/
func stallFollower(f *simulation.PetFollower, n int, nowMs int64) {
	refuse := func(from, _ simulation.Spawn) (simulation.Spawn, *simulation.MoveError) {
		return from, &simulation.MoveError{Reason: "walled in"}
	}
	far := simulation.Spawn{RegionID: 0x62a8, X: 1500, Z: 500}
	for i := 0; i < n; i++ {
		f.Approach(far, 50, nowMs+int64(i)*100, simulation.PetFollowDistance, refuse)
	}
}

/*
================
TestStuckGateByCOSKind

5599C0's gate (CGObj_IsAttackOrPickupCOS 4838C0 -> 44B460, CGObj_IsFellowCOS
483980): attack (3), pickup (4) and fellow (5) COS are relocated onto their
owner; a captured COS (6) only drops its order and stays. Outside BATTLE.
================
*/
func TestStuckGateByCOSKind(t *testing.T) {
	for _, band := range []uint16{3, 4, 5, 6} {
		rt, key, state := strandedPet(t, band, stuckOwnerAt)
		state.combat = nil
		stallFollower(state.follower, 10, 2_000_000)
		frames := rt.answerPetStuck(key, state, state.character.Snapshot(), state.character.ActiveCOS, band<<11|0x1c6, 2_001_000)
		at := state.follower.Position(2_001_000)
		warped := at.RegionID == stuckOwnerAt.RegionID && at.X == stuckOwnerAt.X
		if want := band != 6; warped != want {
			t.Fatalf("band %d: warped %v, want %v (at %+v, frames %d)", band, warped, want, at, len(frames))
		}
		if band != 6 && state.relocatedAtMs != 2_001_000 {
			t.Fatalf("band %d: relocation not published", band)
		}
	}
}

/*
================
TestStuckNeedsTenStalledStopsAndClearsOnStateChange

Nine stalled stops raise nothing; the count survives until a state change
(53FF30, here entering BATTLE through resetPetStuck) and then starts over.
================
*/
func TestStuckNeedsTenStalledStopsAndClearsOnStateChange(t *testing.T) {
	rt, key, state := strandedWolf(t, stuckOwnerAt)
	snapshot, pet, tid := state.character.Snapshot(), state.character.ActiveCOS, uint16(3<<11|0x1c6)
	stallFollower(state.follower, 9, 2_000_000)
	if frames := rt.answerPetStuck(key, state, snapshot, pet, tid, 2_001_000); len(frames) != 0 || state.relocatedAtMs != 0 {
		t.Fatal("nine stalled stops warped the pet")
	}
	resetPetStuck(state)
	stallFollower(state.follower, 9, 2_002_000)
	if rt.answerPetStuck(key, state, snapshot, pet, tid, 2_003_000); state.relocatedAtMs != 0 {
		t.Fatal("the count survived a state change")
	}
	stallFollower(state.follower, 1, 2_003_000)
	if rt.answerPetStuck(key, state, snapshot, pet, tid, 2_004_000); state.relocatedAtMs != 2_004_000 {
		t.Fatal("the tenth stalled stop after the reset did not warp the pet")
	}
}

/*
================
TestHorsesAndTransportsNeverStuckWarp

A ride horse (band 1) and a trade transport (band 2) run no follower AI
unmounted in the port, and 5599C0's gate excludes them: they never warp.
================
*/
func TestHorsesAndTransportsNeverStuckWarp(t *testing.T) {
	for _, band := range []uint16{1, 2} {
		rt, _, c, _ := newPetCombatRuntime(t, 1_000_000, band)
		rt.ConstrainMovement = func(_ string, from, _ simulation.Spawn) (simulation.Spawn, *simulation.MoveError) {
			return from, &simulation.MoveError{Reason: "walled in"}
		}
		gid := c.ActiveCOS.GID
		state := rt.petSessionFor(testDivision, c.Name, gid)
		key := petOwnerKey{division: testDivision, name: lowerName(c), gid: gid}
		for i := 0; i < 50; i++ {
			rt.advancePet(key, int64(2_000_000+i*100))
		}
		if state.relocatedAtMs != 0 || state.follower != nil {
			t.Fatalf("band %d: relocated at %d, follower %v", band, state.relocatedAtMs, state.follower != nil)
		}
	}
}

/*
================
TestPetInBattleKeepsFighting

BATTLE overrides the stuck handler (55A250): with no warp-to-target trace
mode the pet stays in BATTLE where it is.
================
*/
func TestPetInBattleKeepsFighting(t *testing.T) {
	rt, key, state := strandedWolf(t, stuckOwnerAt)
	state.combat = &petCombatIntent{target: 0xdead, nextAttackMs: 1 << 62}
	state.follower.ResetStuck()
	for i := 0; i < 30; i++ {
		rt.answerPetStuck(key, state, state.character.Snapshot(), state.character.ActiveCOS, 3<<11|0x1c6, int64(2_000_000+i*100))
	}
	if state.relocatedAtMs != 0 || state.combat == nil {
		t.Fatal("a pet in BATTLE was warped or left BATTLE")
	}
}

/*
================
TestPetFollowingFreelyNeverStuckWarps

The soak: an owner walking an open field in short and long legs, the pet
following through 1000 ticks with nothing in its way. Every time it reaches
its formation FOLLOW changes state and clears the count (55A960 ->
5599A0), so its short catch-up moves never add up to a warp.
================
*/
func TestPetFollowingFreelyNeverStuckWarps(t *testing.T) {
	rt, _, c, _ := newPetCombatRuntime(t, 1_000_000, attackPetBand)
	gid := c.ActiveCOS.GID
	state := rt.petSessionFor(testDivision, c.Name, gid)
	key := petOwnerKey{division: testDivision, name: lowerName(c), gid: gid}
	worldKey := simulation.WorldKey(testDivision, c.Name)
	x := 500.0
	for i := 0; i < 1000; i++ {
		now := int64(2_000_000 + i*100)
		// Every second the owner steps 15 (a short nudge) or 120 (a walk).
		if i%10 == 0 {
			step := 15.0
			if i%40 == 0 {
				step = 120
			}
			x += step
			if x > 1800 {
				x = 200
			}
			owner := simulation.Spawn{RegionID: 0x62a8, X: x, Z: 500}
			rt.Worlds.Update(worldKey, func() simulation.WorldState { return simulation.SeedWorldState(c) },
				func(w *simulation.WorldState) { w.Spawn, w.MoveSegment = owner, nil })
		}
		rt.advancePet(key, now)
		if state.relocatedAtMs != 0 {
			t.Fatalf("a freely following pet was warped on tick %d", i)
		}
	}
}

/*
================
lowerName
================
*/
func lowerName(c *enterworld.Character) string {
	return strings.ToLower(c.Name)
}
