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

===========================================================================
*/
package action

import (
	"testing"
	"time"

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
