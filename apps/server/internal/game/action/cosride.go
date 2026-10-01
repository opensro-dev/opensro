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

// CCOSManager_TryBindRideActor 4FC636 compares the 3D distance against 30.
const cosMountRange = 30

/*
================
HandleCosRide

CNetProcess_SendCosRideToggle74B5 (6FFB40) writes a state byte then one GID.
Malformed, foreign and repeated requests cannot alter the current mover.
================
*/
func (rt *Runtime) HandleCosRide(division string, c *enterworld.Character, payload []byte) OpResult {
	if c == nil || len(payload) != 5 || payload[0] > 1 {
		return OpResult{}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	snapshot, ref := rt.commandCOSSnapshot(division, c, binary.LittleEndian.Uint32(payload[1:]))
	if snapshot == nil || ref.TidWord>>11 != 2 {
		return OpResult{}
	}
	return rt.changeCosRide(division, c, snapshot, payload[0] == 1)
}

/*
================
changeCosRide

4FC4D0 rejects a remote vehicle; 4FC6D0 detaches the rider and restores its
own movement parameters. Settle the shared mover before changing its owner,
otherwise a dismounted transport follows later player movement on re-entry.
The caller holds the division operation lock.
================
*/
func (rt *Runtime) changeCosRide(division string, c, snapshot *enterworld.Character, mounted bool) OpResult {
	pet := snapshot.ActiveCOS
	if pet.Mounted == mounted {
		return OpResult{}
	}
	now := rt.Now().UnixMilli()
	if mounted {
		owner := rt.liveSpawn(simulation.WorldKey(division, c.Name), snapshot, now)
		vehicle := rt.cosLiveSpawn(division, snapshot, now)
		// 4FC5D9 rejects opposite coordinate planes before measuring range;
		// the shared planar distance intentionally strips the dungeon bit.
		if !worldgeom.SamePlane(owner.RegionID, vehicle.RegionID) {
			return OpResult{}
		}
		distance := math.Hypot(simulation.WorldDistance2D(owner, vehicle), owner.Y-vehicle.Y)
		if !(distance <= cosMountRange) || snapshot.NativeBodyStatus == 6 || snapshot.NativeBodyStatus == 7 {
			return OpResult{}
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
		} else if c.TransformMode == 1 {
			rt.endTransform(division, c, now)
		}
		c.ActiveCOS.Mounted = mounted
		frames = append(frames, wire.Frame{Opcode: wire.OpCosRideState,
			Payload: wire.EncodeCosRideState(enterworld.ObjectIDForCharacter(c), mounted, pet.GID)})
		if mounted {
			// The already visible vehicle owns its speed publication.
			rt.refreshCosAbnormalSpeed(rt.newCosAbnormalOwner(division, c, now))
		} else {
			frames = append(frames, rt.refreshMovementEffects(division, c, now)...)
		}
		return true
	})
	if !committed {
		return OpResult{}
	}
	return OpResult{Frames: frames, Broadcast: frames}
}
