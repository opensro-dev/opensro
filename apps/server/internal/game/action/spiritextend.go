/*
===========================================================================

spiritextend.go - the extension gear lengthens a Devil's Spirit's clock

Port-only, not native to v1.150 (SRO_NASRUN, #3456 slice 4). The use
shape is the v1.188 server's gear case (3/3/13/16): the request's tail
is one byte, the bag slot the gear was dropped on, as the v1.150 gender
tool's. The v1.188 server refuses a slot outside the bag (0x1882), an
empty or non-gear target (3) and a target that was never woken (0x18F8);
it then writes the new expiry and sends 0x3078 {2, slot, seconds left},
which the v1.150 client has no handler for, so the port sends the use
answer alone. The span and ceiling are the owner's (nasrun/extension.go);
a gear refused at the ceiling stays in the bag.

The gear acts on a Spirit only (inferred): the reference server admits
any gear with a stored time, and in this port only a Spirit stores one.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/nasrun"
)

// The gear's refusals, the low bytes of the v1.188 codes.
const (
	spiritExtendErrSlot   uint8 = 0x82 // 0x1882: not a bag slot
	spiritExtendErrTarget uint8 = 0x03 // 3: no item, or not a Spirit
	spiritExtendErrAsleep uint8 = 0xF8 // 0x18F8: never woken
)

/*
================
useSpiritExtension

Runs inside HandleItemUse's door. A gear refused at the ceiling answers
the generic refusal: no native code names it, and the v1.150 notice
table has none of these, so the client shows nothing for any of them.
================
*/
func (rt *Runtime) useSpiritExtension(use skillItemUse, c *enterworld.Character, tail []byte, result *OpResult) bool {
	if !rt.NasrunSocket {
		result.DiagnosticRefusal = "item-use: the Devil's Spirit is off (" + nasrun.Env + ")"
		return false
	}
	r := wire.NewReader(tail)
	target, err := r.U8()
	if err != nil || r.Done() != nil || !inventory.IsBagSlot(target, inventory.BagEnd(c)) {
		*result = itemUseFailure(spiritExtendErrSlot)
		return false
	}
	index := -1
	for i, row := range c.MissionInventory {
		if row.Slot == int64(target) {
			index = i
			break
		}
	}
	if index < 0 || !nasrun.IsSpirit(c.MissionInventory[index].TypeFlags) {
		*result = itemUseFailure(spiritExtendErrTarget)
		return false
	}
	spirit := &c.MissionInventory[index]
	if spirit.Data == 0 {
		*result = itemUseFailure(spiritExtendErrAsleep)
		return false
	}
	next, ok := nasrun.ExtendWithin(spirit.Data, rt.Now().Unix())
	if !ok {
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		result.DiagnosticRefusal = "item-use: the Spirit's clock is at its ceiling"
		return false
	}
	// Write the Spirit while index still names it: spending the gear's
	// last unit rebuilds the bag without its row.
	spirit.Data = next
	remaining := rt.consumeItemUseRow(c, use.row)
	*result = OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemUseResponse,
		Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)},
		spiritClockFrame(target, next)}}
	return true
}
