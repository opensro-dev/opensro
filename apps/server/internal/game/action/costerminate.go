/*
===========================================================================

costerminate.go - the Clean command: a vehicle's permanent retirement

The COS command bar's Clean button (CICCos_ExecuteActionCommand 6A2350,
command 5; a riding mount first asks UIIT_MSG_COS_CLEAN_CONFIRM) sends
0x7618 [u32 gid], and CPSMission_OnCosCleanupResponseB618 (7782A0) reads
the answer. v1.188 serves the same request as 0x70C6,
CGObjPC_HandleCOSTerminateRequest70C6 (511E00):

  - the gid must name one of the owner's records, else error 5;
  - an attack or pickup pet (CGObj_IsAttackOrPickupCOS, TID4 3/4) is
    refused with 5; those leave through cancellation (0x756C);
  - a transport (CGObj_IsVehicleCOS, TID4 2) drops its cargo first
    (CGObjCOS_DropTransportCargo 4D1FD0: the goods go offline, then publish
    unowned, the way a monster's drops do);
  - CCOSManager_RemoveRecordAndRetire (4FBE20) retires with kind 1: the
    rider is dismounted (CCOSManager_DetachManagedActor 4FBDD0), the actor
    despawns, and the COS database row is deleted.

Unlike cancellation, terminate does not check the owner's life, the range
or the vehicle's HP: 511E00 asks only that the record exists.

===========================================================================
*/
package action

import (
	"encoding/binary"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
)

const (
	opCosTerminateRequest  uint16 = 0x7618
	opCosTerminateResponse uint16 = 0xB618
	// 511ED2 pushes 5 for an unknown record or a pet.
	cosTerminateInvalid uint8 = 5
	// 511E7B seeds 4FBE20's error out-parameter with 2.
	cosTerminateFailed uint8  = 2
	cosTerminateOK     uint8  = 1
	cosBandRiding      uint16 = 1
	cosBandTransport   uint16 = 2
	cosBandAttackPet   uint16 = 3
	cosBandPickupPet   uint16 = 4
	// Every characterdata COS_C_/COS_T_ row authors rarity 0 (column 15),
	// so cargo scatters at the base 8..20 radius.
	cosCargoRarity uint8 = 0
)

/*
================
cosTerminateResult

7782A0 reads a result byte; anything but 1 is followed by one failure byte
shown in notification category 12, the same grammar as 0xB56C. v1.188
writes the code as a u16; its low byte is what the v1.150 client reads.
================
*/
func cosTerminateResult(code uint8) OpResult {
	payload := []byte{cosTerminateOK}
	if code != cosTerminateOK {
		payload = []byte{2, code}
	}
	return OpResult{Frames: []wire.Frame{{Opcode: opCosTerminateResponse, Payload: payload}}}
}

/*
================
HandleCosTerminate

Only the vehicle record (ActiveCOS) can pass the band gate: attack and
pickup pets are the companions that live on summoner items. That is also
why 4FA8B0's kind-1 inventory branch has nothing to do here: a riding or
transport summoner is consumed when it is used.
================
*/
func (rt *Runtime) HandleCosTerminate(division string, c *enterworld.Character, payload []byte) OpResult {
	if c == nil || len(payload) != 4 {
		return cosTerminateResult(cosTerminateInvalid)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	gid := binary.LittleEndian.Uint32(payload)
	snapshot := rt.characterSnapshot(division, c)
	if snapshot == nil || snapshot.DeletePending {
		return cosTerminateResult(cosTerminateInvalid)
	}
	cos := snapshot.CompanionByGID(gid)
	refs, ok := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
	if cos == nil || !ok {
		return cosTerminateResult(cosTerminateInvalid)
	}
	ref, found := refs.CharacterRefByCodename(cos.Codename)
	if !found || ref == nil || ref.RefObjID != cos.RefObjID {
		return cosTerminateResult(cosTerminateInvalid)
	}
	band := ref.TidWord >> 11
	if band == cosBandAttackPet || band == cosBandPickupPet ||
		snapshot.ActiveCOS == nil || snapshot.ActiveCOS.GID != gid {
		return cosTerminateResult(cosTerminateInvalid)
	}

	var frames, public []wire.Frame
	if cos.Mounted {
		// 4FBDD0 dismounts before the despawn; the shared ride owner settles
		// the vehicle and restores the rider's own movement.
		ride := rt.changeCosRide(division, c, snapshot, false)
		if len(ride.Broadcast) == 0 {
			return cosTerminateResult(cosTerminateFailed)
		}
		frames = append(frames, ride.Frames...)
		public = append(public, ride.Broadcast...)
		// 4EC78B already retired the riding horse with its dismount.
		if band == cosBandRiding {
			result := cosTerminateResult(cosTerminateOK)
			return OpResult{Frames: append(frames, result.Frames...), Broadcast: public}
		}
	}

	now := rt.Now()
	at := rt.cosLiveSpawn(division, c, now.UnixMilli())
	var dropped []grounditem.Item
	if !rt.deps.Update(c, "cos-terminate", func() bool {
		live := c.ActiveCOS
		if c.DeletePending || live == nil || !live.Summoned || live.GID != gid || live.Mounted {
			return false
		}
		dropped = dropped[:0]
		if band == cosBandTransport && live.Container != nil {
			for _, item := range invItemsFromRowsWithin(live.Container.Rows, int64(live.Container.Capacity)) {
				planned := rt.scatterMonsterDrop(PlanItemDrop(item, item.Quantity, at, c.Name, now), at, cosCargoRarity)
				added := rt.addCharacterGround(division, c, planned)
				if added.Gid == 0 {
					for _, undo := range dropped {
						rt.Ground.Remove(division, undo.Gid)
					}
					return false
				}
				dropped = append(dropped, added)
			}
		}
		c.ActiveCOS = nil
		return true
	}) {
		return cosTerminateResult(cosTerminateFailed)
	}

	if len(dropped) > 0 {
		cargo := rt.groundReferences(dropped)
		for _, item := range dropped {
			cargo = append(cargo, wire.DropBroadcastFrames(item.SpawnRow(true))...)
		}
		frames = append(frames, cargo...)
		public = append(public, cargo...)
	}
	frames = append(frames, rt.retireCosRuntime(division, c, gid)...)
	despawn := wire.Frame{Opcode: wire.OpObjectDespawn, Payload: wire.ObjectDespawn{Gid: gid}.Encode()}
	frames = append(frames, despawn)
	public = append(public, despawn)
	result := cosTerminateResult(cosTerminateOK)
	return OpResult{Frames: append(frames, result.Frames...), Broadcast: public}
}
