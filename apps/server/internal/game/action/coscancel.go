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
	if snapshot.NativeTeleportMode != 0 || snapshot.ActiveCOS.Mounted {
		return cosCancelResult(0x0D)
	}
	now := rt.Now().UnixMilli()
	owner := rt.liveSpawn(simulation.WorldKey(division, c.Name), snapshot, now)
	pet := rt.cosLiveSpawn(division, snapshot, now)
	if !worldgeom.SamePlane(owner.RegionID, pet.RegionID) ||
		!(simulation.WorldDistance2D(owner, pet) < cosCancelRange) {
		return cosCancelResult(4)
	}
	if !rt.deps.Update(c, "cos-cancel", func() bool {
		cos := c.ActiveCOS
		if c.DeletePending || cos == nil || !cos.Summoned || cos.GID != gid || cos.Mounted {
			return false
		}
		cos.Summoned = false
		cos.StateFlags &^= cosStateSummoned
		return true
	}) {
		return cosCancelResult(2)
	}
	result := cosCancelResult(1)
	rt.petMu.Lock()
	state := rt.petSessions[petOwnerKey{division, strings.ToLower(c.Name)}]
	rt.petMu.Unlock()
	if state != nil {
		if state.pickup != nil {
			pending := finishPendingCosPickup(state, failureResult(wire.ErrCodeInvalidRequest))
			result.Frames = append(result.Frames, pending.Frames...)
		}
		state.follower = nil
		state.transportCOS = nil
		state.public = nil
		state.generation++
	}
	rt.storeCosAbnormal(division, c.Name, gid, nil)
	despawn := wire.Frame{Opcode: wire.OpObjectDespawn, Payload: wire.ObjectDespawn{Gid: gid}.Encode()}
	result.Frames = append(result.Frames, despawn)
	result.Broadcast = []wire.Frame{despawn}
	return result
}
