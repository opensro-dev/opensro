/*
===========================================================================

petarrival_test.go - a travelled pet arrives, it does not just appear in scope

After a return scroll or a gate the owner's pets are placed by the
bootstrap list, whose grouped records carry no sub-state byte. The
arrival frames and the peer lane's first sight must still present the
pet as an arrival: one spawn with sub-state 1, the byte the client
answers with the appear effect and the summon sound.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
arrivalSpawnStates

The trailing sub-state byte of every single-spawn frame, by pet gid.
================
*/
func arrivalSpawnStates(t *testing.T, frames []wire.Frame) map[uint32]byte {
	t.Helper()
	states := make(map[uint32]byte)
	for _, frame := range frames {
		if frame.Opcode != wire.OpSingleObjectSpawn {
			continue
		}
		if len(frame.Payload) < 8 {
			t.Fatalf("short spawn payload %d", len(frame.Payload))
		}
		states[binary.LittleEndian.Uint32(frame.Payload[4:])] = frame.Payload[len(frame.Payload)-1]
	}
	return states
}

/*
================
TestTravelledPetsArriveWithSubStateOne
================
*/
func TestTravelledPetsArriveWithSubStateOne(t *testing.T) {
	c, refs := persistentSummonFixture()
	rt, clock := newTestRuntime(c, refs)
	rt.CompanionRoll = func() (uint32, error) { return 0, nil }
	rt.BindPetSession(testDivision, c, 101)
	useSummonerFixture(t, rt, c, 23, refs.staticItemSource["SUMMON_ATTACK"])
	useSummonerFixture(t, rt, c, 24, refs.staticItemSource["SUMMON_PICKUP"])
	gids := make(map[uint32]bool)
	for _, pet := range c.Companions() {
		gids[pet.GID] = true
	}
	if len(gids) != 2 {
		t.Fatalf("fixture summoned %d pets, want 2", len(gids))
	}
	// The summon's own appear window lapses: from here the pets are
	// merely in scope, so any sub-state 1 below is the arrival's.
	clock.Advance(2 * time.Second)
	if frames := rt.petArrivalFrames(testDivision, c); len(frames) != 0 {
		t.Fatal("pets that never travelled produced arrival frames")
	}
	for _, view := range rt.CompanionPresentations(testDivision, c.Name) {
		if view.Fresh {
			t.Fatal("a settled pet still presents as fresh")
		}
	}

	previous := rt.relocateReturningPet(testDivision, c, simulation.Spawn{RegionID: 0x62aa, X: 300, Y: 20, Z: 400})
	states := arrivalSpawnStates(t, rt.petArrivalFrames(testDivision, c))
	if len(states) != len(gids) {
		t.Fatalf("arrival presented %d pets, want %d", len(states), len(gids))
	}
	for gid, state := range states {
		if !gids[gid] || state != 1 {
			t.Fatalf("pet %d arrival sub-state = %d, want 1", gid, state)
		}
	}
	// A peer first seeing the pets at the destination gets the same
	// arrival byte on the spawn the peer lane sends.
	peers := rt.CompanionPresentations(testDivision, c.Name)
	if len(peers) != len(gids) {
		t.Fatalf("peer presentations = %d, want %d", len(peers), len(gids))
	}
	for _, view := range peers {
		if !view.Fresh {
			t.Fatal("a travelled pet does not present as an arrival to peers")
		}
		spawn := view.SpawnFrames(clock.NowMs())
		wireSpawn := make([]wire.Frame, len(spawn))
		for i, frame := range spawn {
			wireSpawn[i] = wire.Frame{Opcode: frame.Opcode, Payload: frame.Payload}
		}
		for gid, state := range arrivalSpawnStates(t, wireSpawn) {
			if state != 1 {
				t.Fatalf("peer spawn of pet %d carries sub-state %d, want 1", gid, state)
			}
		}
	}

	// A refused travel restores the old sessions: no arrival is pending.
	rt.restoreCompanionRelocation(previous)
	if frames := rt.petArrivalFrames(testDivision, c); len(frames) != 0 {
		t.Fatal("a rolled-back relocation still presents an arrival")
	}

	// Once the arrival window lapses the pets are in scope again.
	rt.relocateReturningPet(testDivision, c, simulation.Spawn{RegionID: 0x62aa, X: 300, Y: 20, Z: 400})
	clock.Advance(2 * time.Second)
	if frames := rt.petArrivalFrames(testDivision, c); len(frames) != 0 {
		t.Fatal("a lapsed arrival still produces arrival frames")
	}
	for _, view := range rt.CompanionPresentations(testDivision, c.Name) {
		if view.Fresh {
			t.Fatal("a lapsed arrival still presents as fresh to peers")
		}
	}
}
