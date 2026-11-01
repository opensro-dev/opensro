/*
===========================================================================

petstuck_test.go - tests for petstuck.go: which COS the stuck rule warps

The native stuck rule relocates an attack, pickup or
fellow COS onto its owner after ten stalled stops, in FOLLOW and in PICKITEM
alike; BATTLE keeps fighting, and a pet that keeps reaching its
formation never accumulates the count.

===========================================================================
*/

package action

import (
	"math"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// stuckOwnerAt is an owner one sector from the stranded pet's region
// 0x62a8, inside the native sector adjacency, so only the stuck rule can warp.
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

A pickup pet (band 4) follows through the same FOLLOW state and
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

PICKITEM uses the base stuck handler too: a pickup pet walled away
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

Drive a follower through n refused steps (each a stalled stop, event
8 counted as stuck) toward a far goal, starting at nowMs.
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

The stuck handler's gate (the attack-or-pickup and fellow COS tests
): attack (3), pickup (4) and fellow (5) COS are relocated onto their
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
(here entering BATTLE through resetPetStuck) and then starts over.
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
unmounted in the port, and the stuck handler's gate excludes them: they never warp.
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

BATTLE overrides the stuck handler: with no warp-to-target trace
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
its formation FOLLOW changes state and clears the count (the FOLLOW
tick enters a new state), so its short catch-up moves never add up to a warp.
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

/*
================
newPetPair

An owner with an attack pet (the active COS) and a pickup pet (a summoned
mission-inventory pet, band 4) both following on an open field.
================
*/
func newPetPair(t *testing.T) (*Runtime, *enterworld.Character, []*petSession) {
	t.Helper()
	rt, _, c, _ := newPetCombatRuntime(t, 1_000_000, attackPetBand)
	items := rt.deps.(*enterworld.Deps).Items.(cosTestItemSource)
	pickup := &enterworld.CharacterRef{RefObjID: 10, Codename: "PICKPET", TidWord: 4<<11 | 0x1c6,
		Level: 1, MaxHP: 1_000_000, MaxMP: 600, WalkSpeed: 20, RunSpeed: 80,
		Parameters: monster.MonsterRef{CombatPinned: true, BodyRadius: 10, HitRate: 10}}
	items.characters[pickup.Codename] = pickup
	pet := &enterworld.CharacterCOS{GID: c.ActiveCOS.GID + 0x10000, RefObjID: pickup.RefObjID, Codename: pickup.Codename,
		Level: 1, CurrentHP: pickup.MaxHP, CurrentMP: pickup.MaxMP, StateFlags: 1, Summoned: true,
		Container: &domain.COSContainer{Capacity: 2}}
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 25, RefObjID: 9001, Codename: "ITEM_PICKPET",
		StackCount: 1, Summon: pet})
	rt.BindPetSession(testDivision, c, 2)
	var pets []*petSession
	for _, gid := range []uint32{c.ActiveCOS.GID, pet.GID} {
		state := rt.petSessionFor(testDivision, c.Name, gid)
		if state == nil {
			t.Fatalf("fixture: no session for pet %#x", gid)
		}
		pets = append(pets, state)
	}
	return rt, c, pets
}

/*
================
soakPetPair

Advance both pets 1000 ticks of 100 ms with the owner placed by ownerAt
before each tick; fail on any relocation. Returns how many ticks found each
pet standing within PetFollowDistance of its owner, so a soak that never
follows proves nothing.
================
*/
func soakPetPair(t *testing.T, rt *Runtime, c *enterworld.Character, pets []*petSession, ownerAt func(tick int) simulation.Spawn) []int {
	t.Helper()
	worldKey := simulation.WorldKey(testDivision, c.Name)
	near := make([]int, len(pets))
	for i := 0; i < 1000; i++ {
		now := int64(2_000_000 + i*100)
		owner := ownerAt(i)
		rt.Worlds.Update(worldKey, func() simulation.WorldState { return simulation.SeedWorldState(c) },
			func(w *simulation.WorldState) { w.Spawn, w.MoveSegment = owner, nil })
		for p, state := range pets {
			rt.advancePet(petOwnerKey{division: testDivision, name: lowerName(c), gid: state.transportCOS.GID}, now)
			if state.relocatedAtMs != 0 {
				t.Fatalf("pet %d was warped on tick %d (owner %+v)", p, i, owner)
			}
			if state.follower != nil && simulation.WorldDistance2D(state.follower.Position(now), owner) <= 2*simulation.PetFollowDistance {
				near[p]++
			}
		}
	}
	return near
}

/*
================
TestPetsInFormationWithAStillOwnerNeverStuckWarp

The owner stands still for 1000 ticks: once in formation an attack and a
pickup pet issue no moves, so no stop counts and nothing warps.
================
*/
func TestPetsInFormationWithAStillOwnerNeverStuckWarp(t *testing.T) {
	rt, c, pets := newPetPair(t)
	still := simulation.Spawn{RegionID: 0x62a8, X: 520, Z: 520}
	near := soakPetPair(t, rt, c, pets, func(int) simulation.Spawn { return still })
	for p, n := range near {
		if n < 900 {
			t.Fatalf("pet %d stood by its owner on only %d of 1000 ticks", p, n)
		}
	}
}

/*
================
TestPetsFollowingShortHopsAndACirclingOwnerNeverStuckWarp

Stop-start hops under 20 make every catch-up move end within 20 of where it
began - each such stop counts - but every arrival in formation
changes state and clears the count. An owner circling
the pets keeps moving their slots; the slots stay reachable, so neither pet
warps.
================
*/
func TestPetsFollowingShortHopsAndACirclingOwnerNeverStuckWarp(t *testing.T) {
	hops := []float64{15, 19, 12, 18, 16}
	rt, c, pets := newPetPair(t)
	x := 300.0
	near := soakPetPair(t, rt, c, pets, func(tick int) simulation.Spawn {
		// Hop every half second, always onward, so every hop pulls the pets
		// out of formation for a catch-up move shorter than 20.
		if tick%5 == 0 {
			if x += hops[(tick/5)%len(hops)]; x > 1800 {
				x = 300
			}
		}
		return simulation.Spawn{RegionID: 0x62a8, X: x, Z: 500}
	})
	for p, n := range near {
		if n < 500 {
			t.Fatalf("hops: pet %d kept up on only %d of 1000 ticks", p, n)
		}
	}

	rt, c, pets = newPetPair(t)
	centre := simulation.Spawn{RegionID: 0x62a8, X: 900, Z: 900}
	near = soakPetPair(t, rt, c, pets, func(tick int) simulation.Spawn {
		// A 30-unit circle, a twelfth of a turn every 300 ms.
		angle := float64(tick/3) * math.Pi / 6
		return simulation.Spawn{RegionID: centre.RegionID, X: centre.X + 30*math.Cos(angle), Z: centre.Z + 30*math.Sin(angle)}
	})
	for p, n := range near {
		if n < 500 {
			t.Fatalf("circle: pet %d kept up on only %d of 1000 ticks", p, n)
		}
	}
}
