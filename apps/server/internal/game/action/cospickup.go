/*
===========================================================================

cospickup.go - native COS pickup commands share the inventory authority

The client drop manager submits 0x769E/tag 8. Inventory mutation still belongs
to applyCosGroundAt; this adapter owns the command acknowledgement across both
immediate completion and the existing asynchronous pet approach lifecycle.

===========================================================================
*/
package action

import (
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

const (
	cosPickupSucceeded uint8 = 1
	// The native 77DD10 result table retires unavailable targets for code 2.
	// Generic invalid/lifecycle refusals use that client-defined class; the
	// later server's broader error namespace is not copied onto the 1.150 wire.
	cosPickupUnavailable uint8 = 2
	// 77DD10 disables automatic pickup for B4/B5. The original Korean B4
	// text names the pet bag; its English translation swaps the two bags.
	cosPickupBagFull uint8 = 0xB4
)

/*
================
cosPickupAcknowledgement

74FCD0 reads a result byte only for failure, and always reads the item GID
for selector 8. The inventory receipt alone does not clear command pending.
================
*/
func cosPickupAcknowledgement(command wire.CosCommand, code uint8) wire.Frame {
	w := wire.NewWriter(11)
	if code == cosPickupSucceeded {
		w.U8(1).U8(wire.CosCommandPickupTag)
	} else {
		w.U8(2).U8(wire.CosCommandPickupTag).U8(code)
	}
	return wire.Frame{Opcode: wire.OpCosCommandResult, Payload: w.U32(command.CosGid).U32(command.TargetGid).Payload()}
}

/*
================
finishCosPickupCommand

Keep committed item receipts and peer animations intact. An empty result is
a queued approach, so its acknowledgement must wait for the terminal tick.
================
*/
func finishCosPickupCommand(result OpResult, command wire.CosCommand) OpResult {
	if len(result.Frames) == 0 {
		return result
	}
	code := cosPickupUnavailable
	frames := make([]wire.Frame, 0, len(result.Frames)+1)
	for _, frame := range result.Frames {
		if frame.Opcode == wire.OpItemMoveResponse && len(frame.Payload) == 2 &&
			frame.Payload[0] == 2 && frame.Payload[1] == wire.ErrCodeStorageFull {
			code = cosPickupBagFull
		}
		if frame.Opcode == wire.OpItemMoveResponse && len(frame.Payload) > 0 && frame.Payload[0] == 1 {
			code = cosPickupSucceeded
		}
		// A command refusal must not release an unrelated manual inventory
		// request in the browser. Only committed receipts use the B06D lane.
		if frame.Opcode != wire.OpItemMoveResponse || len(frame.Payload) == 0 || frame.Payload[0] != 2 {
			frames = append(frames, frame)
		}
	}
	result.Frames = append(frames, cosPickupAcknowledgement(command, code))
	return result
}

/*
================
handleCosPickupCommand

The division operation lock is held by HandleCosCommand. No client-supplied
GID can bypass the same ownership and inventory checks as manual pet pickup.
================
*/
func (rt *Runtime) handleCosPickupCommand(division string, c *enterworld.Character, command wire.CosCommand) OpResult {
	refused := OpResult{Frames: []wire.Frame{cosPickupAcknowledgement(command, cosPickupUnavailable)}}
	snapshot, ref := rt.commandCOSSnapshot(division, c, command.CosGid)
	if snapshot == nil || snapshot.CompanionByGID(command.CosGid).Mounted || command.TargetGid == 0 || ref.TidWord>>11 != 4 {
		return refused
	}
	rt.petMu.Lock()
	session := rt.petSessions[petOwnerKey{division: division, name: strings.ToLower(c.Name), gid: command.CosGid}]
	rt.petMu.Unlock()
	if session == nil || session.character != c || session.pickup != nil {
		return refused
	}
	request := wire.ItemMoveRequest{MovementType: wire.MoveTypeCosPickup, CosGID: command.CosGid, GroundGID: command.TargetGid}
	result := rt.applyCosGround(division, c, request)
	if session.pickup != nil {
		session.pickupCommand = true
	}
	return finishCosPickupCommand(result, command)
}

/*
================
expireCosPickup

The pet left PICKITEM (battle, displacement, relocation to its owner) with
a pickup still pending. CAIState_PICKITEM_OnExit (55AD60) answers the owner
then: a pending target sends its failure (55ADF0) before the state clears.
Expiring the deadline lets the pet's next tick retire it through
finishPendingCosPickup, which answers the 0xB06D (and the command, when one
asked). Clearing the slot instead left the request unanswered, and the
browser drops the session when a pending 0xB06D outlives its 10 s wait.
================
*/
func expireCosPickup(state *petSession) {
	if state.pickup != nil {
		state.pickupDeadline = 0
	}
}

/*
================
finishPendingCosPickup

Retire the pending protocol before publishing, including death, despawn,
timeout and target-loss paths. Manual inventory requests retain their receipt.
================
*/
func finishPendingCosPickup(state *petSession, result OpResult) OpResult {
	if state.pickup == nil {
		return result
	}
	request := *state.pickup
	state.pickup = nil
	if state.pickupCommand {
		result = finishCosPickupCommand(result, wire.CosCommand{CosGid: request.CosGID, TargetGid: request.GroundGID})
	}
	state.pickupCommand = false
	return result
}
