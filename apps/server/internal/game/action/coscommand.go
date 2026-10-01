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

HandleCosCommand owns the decoded 0x769E subset. Every form first binds
the claimed GID to the character's persisted active COS and its
characterdata record; the wire can never nominate an arbitrary entity.
Movement, steer and stop go to the movement owner (MoveCOS, SteerCOS,
StopCOS); mount and mounted attack stay here.
================
*/
func (rt *Runtime) HandleCosCommand(
	divisionID string,
	character *enterworld.Character,
	payload []byte,
) OpResult {
	command, err := wire.DecodeCosCommand(payload)
	if err != nil || character == nil {
		return OpResult{}
	}

	unlock := rt.lockDivision(divisionID)
	defer unlock()

	snapshot := rt.characterSnapshot(divisionID, character)
	if snapshot == nil || snapshot.DeletePending || snapshot.ActiveCOS == nil ||
		!snapshot.ActiveCOS.Summoned || snapshot.ActiveCOS.GID != command.CosGid {
		return OpResult{}
	}
	characters, ok := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
	if !ok {
		return OpResult{}
	}
	ref, ok := characters.CharacterRefByCodename(snapshot.ActiveCOS.Codename)
	// The vehicle forms serve whatever the rider sits on: a riding horse moves
	// with the same trio as a transport (rideableCOSBand).
	if !ok || ref == nil || ref.RefObjID != snapshot.ActiveCOS.RefObjID || !rideableCOSBand(ref.TidWord>>11) {
		return OpResult{}
	}
	expectedGID, ok := enterworld.CosObjectIDForCharacter(snapshot)
	if !ok || expectedGID != command.CosGid {
		return OpResult{}
	}

	switch command.Tag {
	case wire.CosCommandMovementTag:
		if !snapshot.ActiveCOS.Mounted || snapshot.ActiveCOS.CurrentHP == 0 || rt.MoveCOS == nil || rt.cosMovementBlocked(divisionID, snapshot) {
			return OpResult{}
		}
		rt.bindResidentRegion(simulation.WorldKey(divisionID, character.Name), rt.Now().UnixMilli())
		return OpResult{Frames: rt.MoveCOS(divisionID, character, command.CosGid, command.Movement)}
	case wire.CosCommandSteerTag, wire.CosCommandStopTag:
		// The vehicle's steer/stop pair belongs to the same movement owner
		// as its moves; only the mounted, living vehicle can walk.
		if !snapshot.ActiveCOS.Mounted || snapshot.ActiveCOS.CurrentHP == 0 {
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
		if snapshot.ActiveCOS.Mounted || snapshot.ActiveCOS.CurrentHP == 0 {
			return OpResult{}
		}
		committed := false
		rt.deps.Update(character, "cos-mount", func() bool {
			if character.ActiveCOS == nil || !character.ActiveCOS.Summoned ||
				character.ActiveCOS.GID != command.CosGid || character.ActiveCOS.Mounted ||
				character.ActiveCOS.CurrentHP == 0 {
				return false
			}
			if character.TransformMode == 1 {
				rt.endTransform(divisionID, character, rt.Now().UnixMilli())
			}
			character.ActiveCOS.Mounted = true
			// The COS spawn already publishes its speed pair. Mounting transfers
			// the authoritative mover to that same keeper without a new speed.
			rt.refreshCosAbnormalSpeed(rt.newCosAbnormalOwner(divisionID, character, rt.Now().UnixMilli()))
			committed = true
			return true
		})
		if !committed {
			return OpResult{}
		}
		frame := wire.Frame{
			Opcode: wire.OpCosRideState,
			Payload: wire.EncodeCosRideState(
				enterworld.ObjectIDForCharacter(snapshot),
				true,
				command.CosGid,
			),
		}
		return OpResult{Frames: []wire.Frame{frame}, Broadcast: []wire.Frame{frame}}

	case wire.CosCommandAttackTag:
		if !snapshot.ActiveCOS.Mounted || snapshot.ActiveCOS.CurrentHP == 0 ||
			ref.MountedAttackCapability210 == 0 || command.TargetGid == 0 {
			return OpResult{}
		}
		// After the COS identity/capability gates, mounted and on-foot attacks
		// intentionally converge on the same server-authoritative pursuit,
		// weapon, posture, cooldown, damage and moving-target re-steer machine.
		// The native difference is the admitted C->S composer, not a second
		// damage authority.
		return rt.beginBasicAttack(divisionID, character, wire.BasicAttackEngage{
			TargetGid: command.TargetGid,
		}, rt.Now().UnixMilli())
	default:
		log.Debugf("action: decoded unsupported COS tag 0x%02X", command.Tag)
		return OpResult{}
	}
}
