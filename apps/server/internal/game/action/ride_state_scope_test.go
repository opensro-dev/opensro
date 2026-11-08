/*
===========================================================================

ride_state_scope_test.go - a 0xB4B5 ride state reaches only its rider's
observers

The v1.150 client resolves the rider of a 0xB4B5 apply and writes into it
with no null test (CPSMission_OnCOSRideStateB4B5 777FB2..777FC3:
GidObjectRegistry_FindEntityByGid, then CICharactor_SetMountedActionState
writes [ecx+0x434]), so the native server never sends it to a client that
does not hold the rider. A dismount caused by someone else (a rider killed
by an attacker) rides the attacker's public result; it must still reach only
the rider's observers, not every observer of the attacker (#1613: "Ride
state references absent entity").

#2865: the same message still ended sessions after #1613 (#1620) on the
live build (console group 2026-11-08: 23 hits, 13 players). The journal's
newest events are one flag-0 (dismount) 0xB4B5 each - rider 100685 with
vehicle 0x00C003AD, rider 100837 with vehicle 0x00C00345 - fanning out to
a dozen sessions in regions 23663/23664 within a second, in world and
entering-world phases. #1620 re-keyed the ACTION lane: every 0xB4B5 now
travels under its payload rider through BroadcastObservedFrames. The
abnormal tick lane never passed through it. When a mounted pet dies of a
hostile word, advanceOneCosAbnormal publishes the death under the PET's
gid, and the dismount inside it rode that shared batch; the tick router
admits a shared batch on the source's visibility alone, so a viewer
holding the pet but not its rider received a ride state naming an entity
it does not hold. playerAbnormalDivisionFrames now partitions every
0xB4B5 out of a shared publication and sends it under its payload rider,
exactly as the action lane does; the pet's own death and despawn still
reach the pet's viewers under the pet.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// rideScopeSilence is how long the edge viewer must stay quiet.
const rideScopeSilence = 300 * time.Millisecond

/*
================
TestRideStateReachesOnlyTheRidersObservers
================
*/
func TestRideStateReachesOnlyTheRidersObservers(t *testing.T) {
	attacker := testCharacter()
	rt, _ := newTestRuntime(attacker, testItems())
	rider := attacker.Snapshot()
	rider.ID, rider.Name = 7, "Rider"
	server := wireStartServer(t, rt)

	actor := wireConnect(t, server, testDivision, attacker.Name)
	both := wireConnect(t, server, testDivision, "sees-both")
	edge := wireConnect(t, server, testDivision, "sees-attacker-only")
	wireObserveCharacter(t, server, both, attacker)
	wireObserveCharacter(t, server, both, rider)
	wireObserveCharacter(t, server, edge, attacker)

	const mount = 0x00C001D1
	dismount := wire.EncodeCosRideState(enterworld.ObjectIDForCharacter(rider), false, mount)
	BroadcastObservedFrames(server.Hub, testDivision, actor.sessionID, enterworld.ObjectIDForCharacter(attacker),
		[]wire.Frame{{Opcode: wire.OpCosRideState, Payload: dismount}})

	both.expectFrame(t, wire.OpCosRideState, dismount)
	expectSilenceAll(t, rideScopeSilence, both, edge)
}

/*
================
TestRidersOwnDismountReachesItsObserversOnce

The rider's own result (it died, or dismounted itself) is already sourced by
the rider: its observers get the dismount once, a viewer that does not hold
the rider gets nothing, and the rider's own session, excepted as the actor,
gets it through its own frames rather than the broadcast.
================
*/
func TestRidersOwnDismountReachesItsObserversOnce(t *testing.T) {
	rider := testCharacter()
	rt, _ := newTestRuntime(rider, testItems())
	server := wireStartServer(t, rt)

	self := wireConnect(t, server, testDivision, rider.Name)
	watcher := wireConnect(t, server, testDivision, "sees-rider")
	elsewhere := wireConnect(t, server, testDivision, "sees-nobody")
	wireObserveCharacter(t, server, watcher, rider)

	riderGID := enterworld.ObjectIDForCharacter(rider)
	dismount := wire.EncodeCosRideState(riderGID, false, 0x00C001D1)
	BroadcastObservedFrames(server.Hub, testDivision, self.sessionID, riderGID,
		[]wire.Frame{{Opcode: wire.OpCosRideState, Payload: dismount}})

	watcher.expectFrame(t, wire.OpCosRideState, dismount)
	expectSilenceAll(t, rideScopeSilence, self, watcher, elsewhere)
}

/*
================
TestRideStateInAnAttackersBatchArrivesOnceInOrder

The dismount sits between the attacker's own frames, so the batch splits by
source. A viewer holding both receives every frame once, in the order sent;
a viewer holding only the attacker receives the attacker's frames and not
the dismount.
================
*/
func TestRideStateInAnAttackersBatchArrivesOnceInOrder(t *testing.T) {
	attacker := testCharacter()
	rt, _ := newTestRuntime(attacker, testItems())
	rider := attacker.Snapshot()
	rider.ID, rider.Name = 7, "Rider"
	server := wireStartServer(t, rt)

	actor := wireConnect(t, server, testDivision, attacker.Name)
	both := wireConnect(t, server, testDivision, "sees-both")
	edge := wireConnect(t, server, testDivision, "sees-attacker-only")
	wireObserveCharacter(t, server, both, attacker)
	wireObserveCharacter(t, server, both, rider)
	wireObserveCharacter(t, server, edge, attacker)

	before := wire.Frame{Opcode: wire.OpActionState, Payload: wire.ArmActionState().Encode()}
	dismount := wire.Frame{Opcode: wire.OpCosRideState,
		Payload: wire.EncodeCosRideState(enterworld.ObjectIDForCharacter(rider), false, 0x00C001D1)}
	after := wire.Frame{Opcode: wire.OpActionState, Payload: wire.ReleaseActionState().Encode()}
	BroadcastObservedFrames(server.Hub, testDivision, actor.sessionID, enterworld.ObjectIDForCharacter(attacker),
		[]wire.Frame{before, dismount, after})

	for _, frame := range []wire.Frame{before, dismount, after} {
		both.expectFrame(t, frame.Opcode, frame.Payload)
	}
	for _, frame := range []wire.Frame{before, after} {
		edge.expectFrame(t, frame.Opcode, frame.Payload)
	}
	expectSilenceAll(t, rideScopeSilence, both, edge)
}

/*
================
TestMountedPetDeathRideStateTravelsUnderItsRider

#2865: a mounted pet dying of a hostile word publishes its death under
the pet's gid, and the dismount inside that publication must not ride
along: the tick router admits a shared batch on the source's visibility
alone, so gating the dismount on the pet reaches viewers that hold the
pet but not its rider, and the client ends the session. The dismount
travels under its payload rider (excepting the rider, who takes the
actor copy), while the pet's death and despawn stay under the pet for
the pet's own viewers.
================
*/
func TestMountedPetDeathRideStateTravelsUnderItsRider(t *testing.T) {
	rt, clock, rider, _ := newCombatTestRuntime(t, 100)
	equipCombatTestPet(t, rt, rider, 3)
	rider.ActiveCOS.CurrentHP = 10
	rider.ActiveCOS.Mounted = true
	petGID := rider.ActiveCOS.GID
	riderGID := enterworld.ObjectIDForCharacter(rider)

	source := *rider
	source.ID, source.Name, source.ActiveCOS = rider.ID+1, "PeriodicCaster", nil
	characters := fixtureCharacters(rt.deps.(*enterworld.Deps).Characters)
	characters[testDivision] = append(characters[testDivision], &source)
	record := abnormal.Record{Status: abnormal.Burn, Level: 1, DurationMs: 10000,
		SourceGID: enterworld.ObjectIDForCharacter(&source), SourceName: source.Name, Rate24: 300, Scale20: 1}
	owner := rt.newCosAbnormalOwner(testDivision, rider, clock.NowMs())
	owner.sources = rt.captureAbnormalSources(testDivision, owner.block, []abnormal.Record{record})
	if !owner.block.Apply(owner, record, clock.NowMs()) {
		t.Fatal("burn did not install")
	}
	owner.commit()

	batches := rt.advanceCosAbnormals(clock.NowMs())
	if rider.ActiveCOS.Mounted {
		t.Fatal("the fatal burn left the pet mounted")
	}

	rides, actorRides, petDespawn := 0, 0, false
	for _, batch := range batches {
		for _, frame := range batch.Frames {
			if frame.Opcode == wire.OpObjectDespawn && len(frame.Payload) >= 4 &&
				binary.LittleEndian.Uint32(frame.Payload) == petGID {
				if batch.OnlyCharacterID == 0 && batch.SourceGID != petGID {
					t.Fatalf("pet despawn routed under gid %d, want the pet %d", batch.SourceGID, petGID)
				}
				petDespawn = true
			}
			if frame.Opcode != wire.OpCosRideState {
				continue
			}
			rides++
			named, ok := wire.CosRideStateRider(frame.Payload)
			if !ok || named != riderGID || len(frame.Payload) != 10 || frame.Payload[5] != 0 ||
				binary.LittleEndian.Uint32(frame.Payload[6:10]) != petGID {
				t.Fatalf("ride state payload %x does not name rider %d dismounting pet %d", frame.Payload, riderGID, petGID)
			}
			switch {
			case batch.OnlyCharacterID == rider.ID:
				actorRides++
			case batch.SourceGID != riderGID:
				t.Fatalf("pet-death dismount routed under gid %d (only %d, source %d), want its rider %d",
					batch.SourceGID, batch.OnlyCharacterID, batch.SourceGID, riderGID)
			case batch.ExceptCharacterID != rider.ID:
				t.Fatalf("rider-routed dismount excepts character %d, want the rider %d", batch.ExceptCharacterID, rider.ID)
			}
		}
	}
	if rides != 2 || actorRides != 1 {
		t.Fatalf("ride states %d (actor copies %d), want one rider-routed and one actor copy", rides, actorRides)
	}
	if !petDespawn {
		t.Fatal("the pet's viewers never received its despawn")
	}
}
