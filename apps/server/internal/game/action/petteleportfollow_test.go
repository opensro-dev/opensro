/*
===========================================================================

petteleportfollow_test.go - a travelled pet follows again at the destination

BR-261121-2209-96DB (issue #4474): after a Hotan teleport the player's
pets were not with him. The carry (relocateReturningPet) places them
and the arrival frames show them (petarrival_test.go); this test holds
the other half on the wire: through the runtime's own tick, the
travelled pet walks after its owner at the destination and the owner
receives its movement, for an attack pet and a pickup pet alike.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
placeTeleportOwner

Stand the fixture owner at spawn in the world store, the pose the pet
tick reads (advancePetSession's liveSpawn).
================
*/
func placeTeleportOwner(rt *Runtime, c *enterworld.Character, spawn simulation.Spawn) {
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(w *simulation.WorldState) {
			w.Spawn = spawn
			w.MoveSegment = nil
		})
}

/*
================
tickTeleportRuntime

Run the runtime tick in 100 ms steps; every batch the tick publishes
for the pet's owner is handed to observe.
================
*/
func tickTeleportRuntime(rt *Runtime, clock *fakeClock, steps int, observe func(simulation.DivisionFrames)) {
	for range steps {
		clock.now = clock.now.Add(100 * time.Millisecond)
		for _, batch := range rt.TickHook()(clock.NowMs()) {
			if observe != nil {
				observe(batch)
			}
		}
	}
}

/*
================
followUntilSettled

Tick until the follower stands within follow distance of target;
false when the steps ran out first.
================
*/
func followUntilSettled(rt *Runtime, clock *fakeClock, state *petSession, steps int, target simulation.Spawn) bool {
	for range steps {
		tickTeleportRuntime(rt, clock, 1, nil)
		if simulation.WorldDistance2D(state.mover.AnyFollower().Position(clock.NowMs()), target) <= simulation.PetFollowDistance {
			return true
		}
	}
	return false
}

/*
================
TestTravelledPetFollowsAgainAtTheDestination
================
*/
func TestTravelledPetFollowsAgainAtTheDestination(t *testing.T) {
	for _, band := range []uint16{attackPetBand, 4} {
		rt, clock, c, _ := newPetCombatRuntime(t, 100, band)
		gid := c.ActiveCOS.GID
		state := rt.petSessionFor(testDivision, c.Name, gid)
		if state == nil {
			t.Fatalf("band %d: no pet session", band)
		}
		ownerKey := simulation.WorldKey(testDivision, c.Name)

		// Settled beside the fixture owner, then a walk across the
		// sector: before any teleport the pet follows.
		home := rt.liveSpawn(ownerKey, c, clock.NowMs())
		if !followUntilSettled(rt, clock, state, 100, home) {
			t.Fatalf("band %d: the pet never settled beside its owner before the teleport", band)
		}
		away := home
		away.X += 120
		placeTeleportOwner(rt, c, away)
		if !followUntilSettled(rt, clock, state, 100, away) {
			at := state.mover.AnyFollower().Position(clock.NowMs())
			t.Fatalf("band %d: before the teleport the pet stayed at %+v while its owner walked to %+v", band, at, away)
		}

		// The teleport: the owner lands in Hotan (the report's region)
		// and the carry places the pet beside them.
		hotan := simulation.Spawn{RegionID: 0x5c87, X: 1169, Y: 244, Z: 223}
		placeTeleportOwner(rt, c, hotan)
		rt.relocateReturningPet(testDivision, c, hotan)
		if at := state.mover.AnyFollower().Position(clock.NowMs()); at.RegionID != hotan.RegionID {
			t.Fatalf("band %d: the travelled pet stands in region %#x, want the destination %#x", band, at.RegionID, hotan.RegionID)
		}

		// The owner walks across the plaza; the pet must follow again,
		// and its movement must reach the owner's wire.
		across := hotan
		across.X += 120
		placeTeleportOwner(rt, c, across)
		moves := 0
		settled := false
		for range 100 {
			tickTeleportRuntime(rt, clock, 1, func(batch simulation.DivisionFrames) {
				if batch.OnlyCharacterID != 0 && batch.OnlyCharacterID != c.ID {
					return
				}
				for _, frame := range batch.Frames {
					if frame.Opcode == simulation.OpMovementAck && len(frame.Payload) >= 4 &&
						binary.LittleEndian.Uint32(frame.Payload) == gid {
						moves++
					}
				}
			})
			if simulation.WorldDistance2D(state.mover.AnyFollower().Position(clock.NowMs()), across) <= simulation.PetFollowDistance {
				settled = true
				break
			}
		}
		if !settled {
			at := state.mover.AnyFollower().Position(clock.NowMs())
			t.Fatalf("band %d: after the teleport the pet stayed at %+v while its owner walked to %+v", band, at, across)
		}
		if moves == 0 {
			t.Fatalf("band %d: the pet followed server-side but no movement frame for it reached the owner", band)
		}
	}
}
