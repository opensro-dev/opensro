/*
===========================================================================

cosride.go - riding and getting off the summoned horse or transport

The COS window's ride button sends 0x74B5 [u8 rideState][u32 cosGid]
(client 6FFB40). The v1.188 server takes it in
CGObjPC_HandleMountToggleRequest (5119B0):

	rideState 1: motion state set (vtable +0xFC) -> 5; battle state
	             (+0x30 +0xD) -> 0xB; then CGObjPC_MountCOSAndBroadcast
	             (4EC640) -> CCOSManager_TryBindRideActor (4FC4D0)
	rideState 0: not riding (+0x30 +0xE) -> 5; then
	             CGObjPC_DismountCOSAndBroadcast (4EC750)
	otherwise:   2

TryBindRideActor refuses with 3 (no vehicle GID), 7 (no owned actor),
5 (already riding), 8 (the actor cannot be ridden), 4 (farther than 30
units) and 9 (the rider is not alive). Success broadcasts the ride state
[1][rider][1|0][cos] to the nearby sessions; a refusal answers the rider
alone with [2][code].

Not ported: the hunter-and-cart refusal (0xA) needs the trade job state,
and the vtable +0x5A0 / +0xC0C / motion 6-7 refusals (2, 0xC) have no
state in this port. Port players carry no nonzero motion state (see
skilladmit.go), so the motion gate (5) never refuses here.

===========================================================================
*/

package action

import (
	"math"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// cosRideReach is TryBindRideActor's 30.0 rider-to-vehicle limit (4FC4D0).
const cosRideReach = 30.0

/*
================
rideableCOSBand

The character TID band (+0x0B) of a COS the rider can sit on: a riding
horse (1) or a transport (2). Attack and pickup pets (3, 4) cannot be
ridden (the actor's +0x47C capability, refusal 8).
================
*/
func rideableCOSBand(band uint16) bool {
	return band == 1 || band == 2
}

/*
================
Runtime.HandleCosRideToggle
================
*/
func (rt *Runtime) HandleCosRideToggle(
	divisionID string,
	character *enterworld.Character,
	payload []byte,
) OpResult {
	request, err := wire.DecodeCosRideToggle(payload)
	if err != nil || character == nil {
		return OpResult{}
	}
	unlock := rt.lockDivision(divisionID)
	defer unlock()
	snapshot := rt.characterSnapshot(divisionID, character)
	if snapshot == nil || snapshot.DeletePending {
		return OpResult{}
	}
	switch request.RideState {
	case 1:
		return rt.rideActiveCOS(divisionID, character, snapshot, request.CosGid)
	case 0:
		return rt.getOffActiveCOS(divisionID, character, snapshot)
	}
	return cosRideRefusal(wire.CosRideRefusedRequest)
}

/*
================
cosRideRefusal
================
*/
func cosRideRefusal(code uint8) OpResult {
	frame := wire.Frame{Opcode: wire.OpCosRideState, Payload: wire.EncodeCosRideRefusal(code)}
	return OpResult{Frames: []wire.Frame{frame}, ActorPrivate: []wire.Frame{frame}}
}

/*
================
rideActiveCOS

4EC640 -> 4FC4D0 in its check order.
================
*/
func (rt *Runtime) rideActiveCOS(divisionID string, character, snapshot *enterworld.Character, cosGid uint32) OpResult {
	now := rt.Now().UnixMilli()
	if inBattleState(snapshot, now) {
		return cosRideRefusal(wire.CosRideInBattle)
	}
	if cosGid == 0 {
		return cosRideRefusal(wire.CosRideRefusedNoTarget)
	}
	cos := snapshot.ActiveCOS
	expectedGID, owned := enterworld.CosObjectIDForCharacter(snapshot)
	if cos == nil || !cos.Summoned || !owned || expectedGID != cosGid {
		return cosRideRefusal(wire.CosRideNotMyCOS)
	}
	if cos.Mounted {
		return cosRideRefusal(wire.CosRideRefusedState)
	}
	characters, ok := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
	if !ok {
		return OpResult{}
	}
	ref, ok := characters.CharacterRefByCodename(cos.Codename)
	if !ok || ref == nil || ref.RefObjID != cos.RefObjID || !rideableCOSBand(ref.TidWord>>11) || cos.CurrentHP == 0 {
		return cosRideRefusal(wire.CosRideCannotRide)
	}
	rider := rt.liveSpawn(simulation.WorldKey(divisionID, snapshot.Name), snapshot, now)
	vehicle := rt.cosLiveSpawn(divisionID, snapshot, now)
	if simulation.IsDungeonRegion(rider.RegionID) != simulation.IsDungeonRegion(vehicle.RegionID) ||
		math.Hypot(simulation.WorldDistance2D(rider, vehicle), rider.Y-vehicle.Y) > cosRideReach {
		return cosRideRefusal(wire.CosRideTooFar)
	}
	if !enterworld.CharacterAlive(snapshot) {
		return cosRideRefusal(wire.CosRideRefusedLife)
	}
	committed := false
	rt.deps.Update(character, "cos-ride", func() bool {
		active := character.ActiveCOS
		if active == nil || !active.Summoned || active.GID != cos.GID || active.Mounted ||
			active.CurrentHP == 0 || !enterworld.CharacterAlive(character) {
			return false
		}
		if character.TransformMode == 1 {
			rt.endTransform(divisionID, character, now)
		}
		active.Mounted = true
		// The COS spawn already publishes its speed pair. Mounting transfers
		// the authoritative mover to that same keeper without a new speed.
		rt.refreshCosAbnormalSpeed(rt.newCosAbnormalOwner(divisionID, character, now))
		committed = true
		return true
	})
	if !committed {
		return cosRideRefusal(wire.CosRideRefusedState)
	}
	frame := wire.Frame{
		Opcode:  wire.OpCosRideState,
		Payload: wire.EncodeCosRideState(enterworld.ObjectIDForCharacter(snapshot), true, cos.GID),
	}
	return OpResult{Frames: []wire.Frame{frame}, Broadcast: []wire.Frame{frame}}
}

/*
================
getOffActiveCOS

4EC750 -> CCOSManager_DismountAndClearRideActor (4FC6D0). Getting off
restores the rider's own movement effects, as the pet-death dismount does.
================
*/
func (rt *Runtime) getOffActiveCOS(divisionID string, character, snapshot *enterworld.Character) OpResult {
	cos := snapshot.ActiveCOS
	if cos == nil || !cos.Mounted {
		return cosRideRefusal(wire.CosRideRefusedState)
	}
	now := rt.Now().UnixMilli()
	var effects []wire.Frame
	committed := false
	rt.deps.Update(character, "cos-get-off", func() bool {
		active := character.ActiveCOS
		if active == nil || active.GID != cos.GID || !active.Mounted {
			return false
		}
		active.Mounted = false
		effects = rt.refreshMovementEffects(divisionID, character, now)
		committed = true
		return true
	})
	if !committed {
		return cosRideRefusal(wire.CosRideRefusedState)
	}
	frame := wire.Frame{
		Opcode:  wire.OpCosRideState,
		Payload: wire.EncodeCosRideState(enterworld.ObjectIDForCharacter(snapshot), false, cos.GID),
	}
	public := append([]wire.Frame{frame}, effects...)
	return OpResult{Frames: public, Broadcast: public}
}
