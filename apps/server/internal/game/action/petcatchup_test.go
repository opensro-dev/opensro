/*
===========================================================================

petcatchup_test.go - a pet left behind reappears at its owner

A player report: a wolf stuck in town stayed there while its owner walked
out. The v1.188 server's follow tick (CAITactics_FollowOwnerFormation
549F80) warps a COS to its owner (CAITactics_RequestPositionRelocation
541110, message 0x7022) once the owner is no longer in its plane and
adjacent sectors (Pos_AreSamePlaneAndAdjacentSectors 430CE0). Within those
sectors it only walks; nothing else teleports it.

===========================================================================
*/
package action

import (
	"strings"
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
strandedWolf

An attack pet whose every step is refused (walled in), with its owner at
ownerAt. Returns the tick key and the pet's session.
================
*/
func strandedWolf(t *testing.T, ownerAt simulation.Spawn) (*Runtime, petOwnerKey, *petSession) {
	t.Helper()
	rt, _, c, _ := newPetCombatRuntime(t, 1_000_000, attackPetBand)
	rt.ConstrainMovement = func(_ string, from, _ simulation.Spawn) (simulation.Spawn, *simulation.MoveError) {
		return from, &simulation.MoveError{Reason: "walled in"}
	}
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(w *simulation.WorldState) {
			w.Spawn = ownerAt
			w.MoveSegment = nil
		})
	gid := c.ActiveCOS.GID
	state := rt.petSessionFor(testDivision, c.Name, gid)
	state.follower = simulation.NewPetFollower(gid, simulation.Spawn{RegionID: 0x62a8, X: 500, Z: 500})
	return rt, petOwnerKey{division: testDivision, name: strings.ToLower(c.Name), gid: gid}, state
}

/*
================
TestStrandedPetWarpsToAnOwnerTwoSectorsAway
================
*/
func TestStrandedPetWarpsToAnOwnerTwoSectorsAway(t *testing.T) {
	owner := simulation.Spawn{RegionID: 0x62aa, X: 300, Z: 500}
	rt, key, state := strandedWolf(t, owner)
	var frames []simulation.Frame
	for now := int64(2_000_000); now < 2_005_000 && state.follower.Position(now).RegionID != owner.RegionID; now += 100 {
		frames = append(frames, rt.advancePet(key, now)...)
	}
	at := state.follower.Position(2_005_000)
	if at.RegionID != owner.RegionID || at.X != owner.X || at.Z != owner.Z {
		t.Fatalf("the walled-in pet stayed at %+v, want a warp to its owner %+v", at, owner)
	}
	despawn, spawn := false, false
	for _, frame := range frames {
		despawn = despawn || frame.Opcode == wire.OpObjectDespawn
		spawn = spawn || frame.Opcode == wire.OpSingleObjectSpawn
	}
	if !despawn || !spawn {
		t.Fatalf("the warp was not shown to observers: %+v", frames)
	}
}

/*
================
TestStrandedPetWalksWhileItsOwnerIsInAnAdjacentSector

430CE0 holds one sector either way: there the pet only walks, so a walled-in
pet stays put, as natively.
================
*/
func TestStrandedPetWalksWhileItsOwnerIsInAnAdjacentSector(t *testing.T) {
	rt, key, state := strandedWolf(t, simulation.Spawn{RegionID: 0x62a9, X: 300, Z: 500})
	for now := int64(2_000_000); now < 2_005_000; now += 100 {
		rt.advancePet(key, now)
	}
	if at := state.follower.Position(2_005_000); at.RegionID != 0x62a8 || at.X != 500 {
		t.Fatalf("a pet with its owner one sector away moved to %+v without a path", at)
	}
}
