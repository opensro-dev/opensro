/*
===========================================================================

coscancel.go - persistent companion cancellation and transient retirement

The authenticated owner retires the live actor, while its durable statistics
and inventory remain available to the summoner. Native 511A60 admits the
request; 4FAAB0 clears the summoned bit without deleting the pet database row.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"strings"

	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// 511A60 uses a strict planar distance below 100, unlike mounting's
	// inclusive 30-unit sphere. Keep the two admission rules independent.
	cosCancelRange             = 100
	cosStateSummoned    uint32 = 2
	opCosCancelRequest  uint16 = 0x756C
	opCosCancelResponse uint16 = 0xB56C
)

/*
================
cosCancelResult

778340 reads a single failure byte in notification category 12. The later
server's 0x44xx errors retain their low byte on the v1.150 response grammar.
================
*/
func cosCancelResult(code uint8) OpResult {
	payload := []byte{1}
	if code != 1 {
		payload = []byte{2, code}
	}
	return OpResult{Frames: []wire.Frame{{Opcode: opCosCancelResponse, Payload: payload}}}
}

/*
================
HandleCosCancel

Never resolve a supplied GID globally: it must name this session's companion.
Refusal leaves both durable state and its in-flight command intact.
================
*/
func (rt *Runtime) HandleCosCancel(division string, c *enterworld.Character, payload []byte) OpResult {
	if c == nil || len(payload) != 4 {
		return cosCancelResult(3)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	gid := binary.LittleEndian.Uint32(payload)
	snapshot, ref := rt.ownedCOSSnapshot(division, c, gid)
	if snapshot == nil || (ref.TidWord>>11 != 3 && ref.TidWord>>11 != 4) {
		return cosCancelResult(3)
	}
	if snapshot.NativeTeleportMode != 0 || snapshot.CompanionByGID(gid).Mounted {
		return cosCancelResult(0x0D)
	}
	now := rt.Now().UnixMilli()
	owner := rt.liveSpawn(simulation.WorldKey(division, c.Name), snapshot, now)
	pet := rt.companionLiveSpawn(division, snapshot, snapshot.CompanionByGID(gid), now)
	if !worldgeom.SamePlane(owner.RegionID, pet.RegionID) ||
		!(simulation.WorldDistance2D(owner, pet) < cosCancelRange) {
		return cosCancelResult(4)
	}
	var cancelled *enterworld.CharacterCOS
	if !rt.deps.Update(c, "cos-cancel", func() bool {
		cos := c.CompanionByGID(gid)
		if c.DeletePending || cos == nil || !cos.Summoned || cos.GID != gid || cos.Mounted {
			return false
		}
		cos.RefreshRentalTimes(now / 1000)
		cos.Summoned = false
		cos.StateFlags &^= cosStateSummoned
		cancelled = cos
		return true
	}) {
		return cosCancelResult(2)
	}
	result := cosCancelResult(1)
	result.Frames = append(result.Frames, rt.retireCosRuntime(division, c, gid)...)
	despawn := wire.Frame{Opcode: wire.OpObjectDespawn, Payload: wire.ObjectDespawn{Gid: gid}.Encode()}
	result.Frames = append(result.Frames, despawn)
	result.Broadcast = []wire.Frame{despawn}
	result.Frames = append(result.Frames, companionItemStateFrames(c, cancelled)...)
	return result
}

/*
================
releaseRiddenVehicleInDoor

CGObjPC_ProcessNormalDeath (529B10) releases the vehicle the player rides
(the COS manager's +0x30, bound by TryBindRideActor and cleared on dismount)
through ReleaseCOSOrExit: a rider who dies loses the horse or transport it
sat on. Its kind-1 release (CCOSManager_RetireOwnedActorAndPersist 4FA8B0,
database operation 1, _DeleteCharCOS) deletes the vehicle's record; the
cargo goes with it, since only the vehicle's own death reaches
DropTransportCargo (cosdeath.go). The caller holds c's door. public
carries the dismount, the speed refresh and the despawn; private the
owner's item state.
================
*/
func (rt *Runtime) releaseRiddenVehicleInDoor(division string, c *enterworld.Character, now int64) (public, private []wire.Frame) {
	ride := c.ActiveCOS
	if ride == nil || !ride.Summoned || !ride.Mounted {
		return nil, nil
	}
	gid := ride.GID
	ride.Mounted = false
	ride.Summoned = false
	ride.StateFlags &^= cosStateSummoned
	public = append(public, wire.Frame{Opcode: wire.OpCosRideState,
		Payload: wire.EncodeCosRideState(enterworld.ObjectIDForCharacter(c), false, gid)})
	public = append(public, rt.refreshMovementEffects(division, c, now)...)
	public = append(public, rt.retireCosRuntime(division, c, gid)...)
	public = append(public, wire.Frame{Opcode: wire.OpObjectDespawn, Payload: wire.ObjectDespawn{Gid: gid}.Encode()})
	private = companionItemStateFrames(c, ride)
	c.ActiveCOS = nil
	return public, private
}

/*
================
retireCompanionCorpses

A successful re-entry (rebirth, return scroll, portal, GM warp) rebuilds the
owner's world. CCOSManager_RestoreLoadedActors (4FA430) admits only records
whose alive and summoned bits are both set: a dead COS stays on its summoner
item for revival and never follows its owner into the new world. Native gives
the teleport no separate branch for corpses, so the re-entry applies the same
admission rule here (inference from 4FA430; restoreCharacterCOS applies it at
login). The caller holds the division operation lock. Returns the owner's
item-state frames and the despawn the old neighbourhood must receive.
================
*/
func (rt *Runtime) retireCompanionCorpses(division string, c *enterworld.Character) (owner, public []wire.Frame) {
	var corpses []*enterworld.CharacterCOS
	rt.deps.Update(c, "cos-reentry-corpses", func() bool {
		corpses = corpses[:0]
		for _, pet := range c.Companions() {
			if !pet.Summoned || pet.CurrentHP != 0 {
				continue
			}
			pet.Summoned = false
			pet.StateFlags &^= cosStateSummoned
			pet.Mounted = false
			corpses = append(corpses, pet)
		}
		return len(corpses) > 0
	})
	for _, pet := range corpses {
		owner = append(owner, rt.retireCosRuntime(division, c, pet.GID)...)
		owner = append(owner, companionItemStateFrames(c, pet)...)
		public = append(public, wire.Frame{Opcode: wire.OpObjectDespawn, Payload: wire.ObjectDespawn{Gid: pet.GID}.Encode()})
	}
	return owner, public
}

/*
================
retireCosRuntime

Retire the actor's transient work after the durable cancellation or horse
removal commits. A later actor reusing its GID must not inherit old effects.
The caller holds the division operation lock.
================
*/
func (rt *Runtime) retireCosRuntime(division string, c *enterworld.Character, gid uint32) []wire.Frame {
	var frames []wire.Frame
	rt.petMu.Lock()
	state := rt.petSessions[petOwnerKey{division: division, name: strings.ToLower(c.Name), gid: gid}]
	rt.petMu.Unlock()
	if state != nil {
		rt.releasePetFormation(petOwnerKey{division: division, name: strings.ToLower(c.Name), gid: gid}, state)
		if state.pickup != nil {
			pending := finishPendingCosPickup(state, failureResult(wire.ErrCodeInvalidRequest))
			frames = append(frames, pending.Frames...)
		}
		state.satiety = companion.SatietyClock{}
		state.follower = nil
		state.transportCOS = nil
		state.public = nil
		state.generation++
	}
	rt.cancelCompanionCasts(division, c, gid)
	rt.storeCosAbnormal(division, c.Name, gid, nil)

	return frames
}
