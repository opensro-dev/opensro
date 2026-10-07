/*
===========================================================================

coscommand.go - the action lane's 0x769E COS command dispatch

===========================================================================
*/
package action

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
Runtime.HandleCosCommand

HandleCosCommand owns decoded 0x769E commands. Every form first binds
the claimed GID to the character's persisted active COS and its
characterdata record; the wire can never nominate an arbitrary entity.
Movement, steer and stop go to the movement owner (MoveCOS, SteerCOS,
StopCOS); pickup and follow use the pet owner, and mount shares the ride door.
================
*/
func (rt *Runtime) HandleCosCommand(
	divisionID string,
	character *enterworld.Character,
	payload []byte,
) OpResult {
	return rt.handleCosCommand(divisionID, character, payload, nil)
}

/*
================
handleCosCommand

Transport supplies a publication sink so mounted movement and its receipt
share the movement owner's ordering boundary. Direct callers retain frames.
================
*/
func (rt *Runtime) handleCosCommand(divisionID string, character *enterworld.Character, payload []byte, publish func([]wire.Frame)) OpResult {
	command, err := wire.DecodeCosCommand(payload)
	if err != nil || character == nil {
		return OpResult{}
	}

	unlock := rt.lockDivision(divisionID)
	defer unlock()
	if command.Tag == wire.CosCommandPickupTag {
		return rt.handleCosPickupCommand(divisionID, character, command)
	}
	if command.Tag == wire.CosCommandFollowTag {
		return rt.handleCosFollowCommand(divisionID, character, command.CosGid)
	}
	if command.Tag == wire.CosCommandAttackTag {
		// 4D2200 serves the attack order for every owned COS: a mounted
		// vehicle with the +0x210 capability carries its rider into battle
		// (below), an attack pet fights on its own AI (petcombat.go).
		if pet, ref := rt.commandCOSSnapshot(divisionID, character, command.CosGid); pet != nil && ref.TidWord>>11 == attackPetBand {
			return rt.orderPetAttack(divisionID, character, pet, command.CosGid, command.TargetGid, rt.Now().UnixMilli())
		}
	}

	snapshot, ref := rt.commandCOSSnapshot(divisionID, character, command.CosGid)
	if snapshot == nil || (ref.TidWord>>11 != 1 && ref.TidWord>>11 != 2) {
		return OpResult{}
	}

	switch command.Tag {
	case wire.CosCommandMovementTag:
		if !snapshot.CompanionByGID(command.CosGid).Mounted || snapshot.CompanionByGID(command.CosGid).CurrentHP == 0 || rt.MoveCOS == nil || rt.cosMovementBlocked(divisionID, snapshot) {
			return OpResult{}
		}
		rt.bindResidentRegion(simulation.WorldKey(divisionID, character.Name), rt.Now().UnixMilli())
		if publish != nil && rt.MoveCOSPublished != nil {
			rt.MoveCOSPublished(divisionID, character, command, publish)
			return OpResult{}
		}
		return OpResult{Frames: rt.MoveCOS(divisionID, character, command.CosGid, command.Movement)}
	case wire.CosCommandSteerTag, wire.CosCommandStopTag:
		// The vehicle's steer/stop pair belongs to the same movement owner
		// as its moves; only the mounted, living vehicle can walk.
		if !snapshot.CompanionByGID(command.CosGid).Mounted || snapshot.CompanionByGID(command.CosGid).CurrentHP == 0 {
			return OpResult{}
		}
		if command.Tag == wire.CosCommandSteerTag && rt.cosMovementBlocked(divisionID, snapshot) {
			return OpResult{}
		}
		handle := rt.SteerCOS
		if command.Tag == wire.CosCommandStopTag {
			handle = rt.StopCOS
		}
		if handle == nil {
			return OpResult{}
		}
		frames, broadcast := handle(divisionID, character, command.CosGid, command.Heading)
		return OpResult{Frames: frames, Broadcast: broadcast}
	case wire.CosCommandMountTag:
		return rt.changeCosRide(divisionID, character, snapshot, true)

	case wire.CosCommandAttackTag:
		// CGObjCOS_DispatchOwnedCommand (4D2200) case 1 validates the target and
		// posts AI event 0x19 to the vehicle's own controller (+0x188 slot
		// 0x1C): the order belongs to the COS, never to its rider. A ride or
		// transport vehicle has no attack skill, so nothing strikes. The client
		// sends this order for any vehicle with characterdata column 88 set
		// (every ride horse carries 3000, 692CB0); it is not a rider-attack
		// licence. Riders dismount to fight.
		return OpResult{DiagnosticRefusal: "mounted-attack-is-the-vehicle-order"}
	default:
		log.Debugf("action: decoded unsupported COS tag 0x%02X", command.Tag)
		return OpResult{}
	}
}

/*
================
commandCOSSnapshot

One identity/liveness gate for every COS command family. Individual commands
then check their native family and mount predicates before changing state.
The caller holds the division operation lock for the entire command.
================
*/
func (rt *Runtime) commandCOSSnapshot(division string, c *enterworld.Character, gid uint32) (*enterworld.Character, *enterworld.CharacterRef) {
	snapshot, ref := rt.ownedCOSSnapshot(division, c, gid)
	if snapshot == nil || snapshot.CompanionByGID(gid).CurrentHP == 0 {
		return nil, nil
	}
	return snapshot, ref
}

/*
================
ownedCOSSnapshot

Cancellation addresses a retained dead pet as well as a living one. Movement,
pickup and attack add their liveness gate through commandCOSSnapshot.
================
*/
func (rt *Runtime) ownedCOSSnapshot(division string, c *enterworld.Character, gid uint32) (*enterworld.Character, *enterworld.CharacterRef) {
	snapshot := rt.characterSnapshot(division, c)
	if snapshot == nil || snapshot.DeletePending || enterworld.CurrentHP(snapshot) == 0 {
		return nil, nil
	}
	cos := snapshot.CompanionByGID(gid)

	if cos == nil || !cos.Summoned || cos.GID != gid {
		return nil, nil
	}
	refs, ok := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
	if !ok {
		return nil, nil
	}
	ref, ok := refs.CharacterRefByCodename(cos.Codename)
	if !ok || ref == nil || ref.RefObjID != cos.RefObjID || ref.TidWord&0x7fe != 0x1c6 {
		return nil, nil
	}
	return snapshot, ref
}
