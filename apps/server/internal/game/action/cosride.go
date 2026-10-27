/*
===========================================================================

cosride.go - shared COS mount and dismount authority

The action pane uses 769E/tag B; the pet panel uses 74B5. Both enter the
same identity, distance and state transition rather than diverging by UI.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"math"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

// The reference server's ride bind compares the 3D distance against 30.
const (
	cosMountRange             = 30
	cosRideBusy         uint8 = 2
	cosRideInvalidActor uint8 = 3
	cosRideOutOfRange   uint8 = 4
	cosRideInvalidState uint8 = 5
	// UIIT_MSG_CMSERR_CANT_GETOFF_FROM_RUNNING_HORSE (category 14, code 6).
	cosRideRunning        uint8 = 6
	cosRideUnknownActor   uint8 = 7
	cosRideNotUsable      uint8 = 8
	cosRideInvalidPosture uint8 = 12
	// UIIT_MSG_COSERR_COS_CAN_NOT_RIDE_BATTLE.
	cosRideInBattle uint8 = 11
)

/*
================
cosRideFailure

Client 777F60 consumes one notification-category-14 byte after the failure marker.
Only the requester receives refusals; observers retain the committed state.
================
*/
func cosRideFailure(code uint8) OpResult {
	return OpResult{Frames: []wire.Frame{{Opcode: wire.OpCosRideState, Payload: []byte{2, code}}}}
}

/*
================
HandleCosRide

The client's CNetProcess_SendCosRideToggle74B5 (6FFB40) writes a state byte then one GID.
Malformed, foreign and repeated requests cannot alter the current mover.
================
*/
func (rt *Runtime) HandleCosRide(division string, c *enterworld.Character, payload []byte) OpResult {
	if c == nil || len(payload) != 5 || payload[0] > 1 {
		return cosRideFailure(cosRideInvalidActor)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	// Before the reference server mounts the vehicle, it refuses a rider whose motion
	// byte is set (moving, seated, changing posture, at a wall, frozen,
	// stunned or asleep) and one in battle state (+0x30 +0xD).
	// Dismount is refused for neither.
	//
	// INFERENCE: v1.188's dismount stops a moving mount instead,
	// but the v1.150 client answers code 6 with
	// CMSERR_CANT_GETOFF_FROM_RUNNING_HORSE ("Can step down while the
	// transport is moving", 777F60 -> category 14), so the v1.150 server
	// refused a step-down while the mount moved: stop first, then dismount.
	if payload[0] == 0 && rt.riderMoving(division, c) {
		return cosRideFailure(cosRideRunning)
	}
	if payload[0] == 1 {
		if rider := rt.characterSnapshot(division, c); rider != nil {
			now := rt.Now().UnixMilli()
			if rt.playerMotionState(division, rider, now) != simulation.MotionNone {
				return cosRideFailure(cosRideInvalidState)
			}
			if inBattleState(rider, now) {
				return cosRideFailure(cosRideInBattle)
			}
		}
	}
	snapshot, ref := rt.commandCOSSnapshot(division, c, binary.LittleEndian.Uint32(payload[1:]))
	if snapshot == nil || (ref.TidWord>>11 != 1 && ref.TidWord>>11 != 2) {
		return cosRideFailure(cosRideUnknownActor)
	}
	return rt.changeCosRide(division, c, snapshot, payload[0] == 1)
}

/*
================
riderMoving

Whether the mount under c is still travelling (the reference server
asks the vehicle): a mounted rider's movement is its own world's.
================
*/
func (rt *Runtime) riderMoving(division string, c *enterworld.Character) bool {
	if rt.Worlds == nil {
		return false
	}
	rider := rt.characterSnapshot(division, c)
	if rider == nil || rider.ActiveCOS == nil || !rider.ActiveCOS.Mounted {
		return false
	}
	world := rt.Worlds.Snapshot(simulation.WorldKey(division, c.Name), func() simulation.WorldState {
		return simulation.SeedWorldState(rider)
	})
	return world.MovingAt(rt.Now().UnixMilli())
}

/*
================
changeCosRide

The reference server rejects a remote vehicle; its dismount detaches the rider and restores its
own movement parameters. Settle the shared mover before changing its owner,
otherwise a dismounted transport follows later player movement on re-entry.
The caller holds the division operation lock.
================
*/
func (rt *Runtime) changeCosRide(division string, c, snapshot *enterworld.Character, mounted bool) OpResult {
	pet := snapshot.ActiveCOS
	refs, ok := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
	if !ok {
		return cosRideFailure(cosRideUnknownActor)
	}
	ref, found := refs.CharacterRefByCodename(pet.Codename)
	if !found || ref == nil || ref.RefObjID != pet.RefObjID {
		return cosRideFailure(cosRideUnknownActor)
	}
	// The reference server asks the shared casting/front-command lock. A mount must
	// not replace the mover while a positive-time action still owns it.
	if mounted && (rt.PlayerAttackLocked(division, c.Name) || rt.objectActionCommitted(division, c.Name)) {
		return cosRideFailure(cosRideBusy)
	}
	// The reference server asks the object's can-use test, which reads the reference flag.
	if mounted && !ref.CanRide {
		return cosRideFailure(cosRideNotUsable)
	}
	if pet.Mounted == mounted {
		return cosRideFailure(cosRideInvalidState)
	}
	now := rt.Now().UnixMilli()
	vehicle := rt.cosLiveSpawn(division, snapshot, now)
	if mounted {
		owner := rt.liveSpawn(simulation.WorldKey(division, c.Name), snapshot, now)
		// The reference server rejects opposite coordinate planes before measuring range;
		// the shared planar distance intentionally strips the dungeon bit.
		if !worldgeom.SamePlane(owner.RegionID, vehicle.RegionID) {
			return cosRideFailure(cosRideOutOfRange)
		}
		distance := math.Hypot(simulation.WorldDistance2D(owner, vehicle), owner.Y-vehicle.Y)
		if !(distance <= cosMountRange) {
			return cosRideFailure(cosRideOutOfRange)
		}
		if snapshot.NativeBodyStatus == 6 || snapshot.NativeBodyStatus == 7 {
			return cosRideFailure(cosRideInvalidPosture)
		}
	}
	var frames []wire.Frame
	committed := rt.deps.Update(c, "cos-ride", func() bool {
		if c.ActiveCOS == nil || !c.ActiveCOS.Summoned || c.ActiveCOS.GID != pet.GID ||
			c.ActiveCOS.CurrentHP == 0 || c.ActiveCOS.Mounted == mounted {
			return false
		}
		if !mounted {
			frames = append(frames, rt.stopCosMovement(division, c, now)...)
			rt.rememberTransportCOS(division, c, rt.cosLiveSpawn(division, c, now))
		} else {
			if c.TransformMode == 1 {
				rt.endTransform(division, c, now)
			}
			// The reference server relocates the owner to the admitted vehicle before binding
			// the ride actor. Keeping the old rider pose moves the vehicle instead.
			world := rt.Worlds.Update(simulation.WorldKey(division, c.Name),
				func() simulation.WorldState { return simulation.SeedWorldState(c) },
				func(state *simulation.WorldState) {
					state.Spawn = vehicle
					state.MoveSegment = nil
					state.SpawnSet = true
					state.MovementSourceSeeded = true
				})
			writeBackWorld(c, world)
			rt.notePlacement(c, "mount")
			frames = append(frames, wire.Frame{Opcode: wire.OpObjectSourceCorrection,
				Payload: wire.ObjectSourceCorrection{Gid: enterworld.ObjectIDForCharacter(c), Position: wire.Position{
					RegionID: vehicle.RegionID, X: float32(vehicle.X), Y: float32(vehicle.Y),
					Z: float32(vehicle.Z), Heading: vehicle.Angle,
				}}.Encode()})
		}
		c.ActiveCOS.Mounted = mounted
		// The reference server retires band-1 horses on dismount. Trade transports keep
		// their detached pose and can be mounted again through this door.
		if !mounted && ref.TidWord>>11 == 1 {
			c.ActiveCOS = nil
			frames = append(frames, wire.Frame{Opcode: wire.OpObjectDespawn,
				Payload: wire.ObjectDespawn{Gid: pet.GID}.Encode()})
		}
		frames = append(frames, wire.Frame{Opcode: wire.OpCosRideState,
			Payload: wire.EncodeCosRideState(enterworld.ObjectIDForCharacter(c), mounted, pet.GID)})
		if mounted {
			// The already visible vehicle owns its speed publication.
			frames = append(frames, rt.refreshCosAbnormalSpeed(rt.newCosAbnormalOwner(division, c, now))...)
		} else {
			frames = append(frames, rt.refreshMovementEffects(division, c, now)...)
		}
		return true
	})
	if !committed {
		return cosRideFailure(cosRideInvalidState)
	}
	public := append([]wire.Frame(nil), frames...)
	if !mounted && ref.TidWord>>11 == 1 {
		frames = append(frames, rt.retireCosRuntime(division, c, pet.GID)...)
		frames = append(frames, cosRemovalNotice(pet.GID))
	}
	return OpResult{Frames: frames, Broadcast: public}
}
