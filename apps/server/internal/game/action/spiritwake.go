/*
===========================================================================

spiritwake.go - using a Devil's Spirit wakes its clock

Port-only, not native to v1.150 (SRO_NASRUN, #3456 step 2); the rule is
the v1.188 server's. A Spirit arrives dormant (data word 0) and cannot be
worn until it is used: the use stamps its expiry, now + its own Param1
seconds (28 days on the mall Spirits, 7 on the event pair), and keeps
the item. A Spirit already woken, awake or lapsed, refuses a second use
as the reference server does (its generic refusal); only the extension
gear lengthens a woken clock. The rule itself is nasrun/clock.go.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/nasrun"
)

// spiritPeriodField is the item row's Param1: the awakening in seconds.
const spiritPeriodField = "itemParam1_29c"

/*
================
wakeSpirit

The use of the Spirit in rowIndex, inside HandleItemUse's door. The item
stays in its slot; the success row echoes its unchanged count.
================
*/
func (rt *Runtime) wakeSpirit(c *enterworld.Character, ref *enterworld.ItemRef, rowIndex int, request wire.ItemUseRequest, result *OpResult) bool {
	if !rt.NasrunSocket {
		result.DiagnosticRefusal = "item-use: the Devil's Spirit is off (" + nasrun.Env + ")"
		return false
	}
	period, _ := ref.NativeFields.Lookup(spiritPeriodField)
	row := &c.MissionInventory[rowIndex]
	next, woke := nasrun.Wake(row.Data, int64(period), rt.Now().Unix())
	if !woke {
		return false
	}
	row.Data = next
	*result = OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemUseResponse,
		Payload: wire.EncodeItemUseSuccess(request.Slot, uint16(row.StackCount), request.TypeWord)}}}
	return true
}
