/*
===========================================================================

itemuse_finish.go - what settles after the item-use door closes

Some uses cannot finish inside the session door: the name change, the
crest delete, the warehouse growth, the guild recall and the reverse
party move commit store or guild work that must never nest inside it,
so their door arms only admit and queue (itemuse.go). This file owns
the settlement that runs once the door has closed: the committed use's
own tail (the revival rebind, then the success visual) and one finish
call per queued use. Each finish spends its item only after its own
outside-door commit lands; a refusal there leaves the item in the bag.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
itemUseFinishes

What one item use leaves for after its door closes. The door arms
fill the queued fields when they admit a use they cannot finish
themselves; the rest describes the committed use's own tail.
================
*/
type itemUseFinishes struct {
	// Whether the door's update committed.
	committed bool
	// A use that revives the player publishes after its commit
	// (revivalFrames rebinds the resident region, which reads the
	// committed character).
	after func()
	// The item the door resolved, spent by each finish and named
	// by the committed use's visual.
	used *enterworld.ItemRef
	// The object the 0x3449 sparkle plays on: the user, unless the
	// item names a pet (petItemVisualTarget).
	visualGID uint32
	// A name-change scroll the closure admitted: the rename queues
	// after this door closes, then the scroll is spent
	// (namechange.go).
	queuedName  string
	queuedNowMs int64
	// A crest delete the closure admitted: the crest clears after
	// this door closes, then the item is spent (crestdelete.go).
	crestDelete string
	// A warehouse addition the closure admitted: the room grows
	// after this door closes, then the item is spent (expansion.go).
	warehouseUse *pendingWarehouseUse
	// A guild recall the closure admitted: the gates judge, the
	// scroll is spent and the guild is asked after this door closes
	// (guildrecall.go).
	recallUse *guildRecallUse
	// A reverse party move the closure admitted: the member's
	// standing point is judged and the cast started after this door
	// closes (reverseuser.go).
	reverseParty *reversePartyMove
}

/*
================
settleItemUseFinishes

Run one use's settlement: the committed tail first (the revival
rebind, then the success visual), then each queued finish. The
finishes run whether or not the door committed - a queued use's door
arm returned false, so its commit belongs out here.
================
*/
func (rt *Runtime) settleItemUseFinishes(divisionID string, character *enterworld.Character, request wire.ItemUseRequest, finishes itemUseFinishes, result *OpResult) {
	if finishes.committed && finishes.after != nil {
		finishes.after()
	}
	if finishes.committed && finishes.used != nil {
		rt.publishItemUseVisual(character, finishes.used, finishes.visualGID, result)
	}
	if finishes.queuedName != "" {
		rt.finishNameChangeUse(divisionID, character, request, finishes.used, finishes.queuedName, finishes.queuedNowMs, result)
	}
	if finishes.crestDelete != "" {
		rt.finishCrestDeleteUse(divisionID, character, request, finishes.used, finishes.crestDelete, result)
	}
	if finishes.warehouseUse != nil {
		rt.finishWarehouseAddition(divisionID, character, finishes.warehouseUse, result)
	}
	if finishes.recallUse != nil {
		rt.finishGuildRecallUse(divisionID, character, request, finishes.used, finishes.recallUse, result)
	}
	if finishes.reverseParty != nil {
		rt.finishReversePartyMove(divisionID, character, request, finishes.used, finishes.reverseParty, result)
	}
}

/*
================
publishItemUseVisual

The native item use (v1.188) ends every successful use the same
way: the 0xB04C success, then 0x305C {gid, item reference} to the nearby
sessions, v1.150's 0x3449 (the client's external item effect, 74F540).
The gid is the user's, unless the item's handler named another object
(petItemVisualTarget); target 0 is the user:
the potion sparkle, the scroll glow, the firework. The native client holds
every item record; the browser holds only the references it was sent, so
both audiences, the user and the observers, get the item's reference right
before the visual names it (peer_references.go). The user's copy matters
when the used row was the last of its kind the browser knew of: a repeat
of a known reference is harmless.
================
*/
func (rt *Runtime) publishItemUseVisual(character *enterworld.Character, ref *enterworld.ItemRef, target uint32, result *OpResult) {
	at := -1
	for index, frame := range result.Frames {
		if frame.Opcode == wire.OpItemUseResponse && len(frame.Payload) > 0 && frame.Payload[0] == wire.ResultSuccess {
			at = index
			break
		}
	}
	if at < 0 {
		return
	}
	reference := rt.commerceReferences([]inventory.Item{{RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags()}}, nil)
	if target == 0 {
		target = enterworld.ObjectIDForCharacter(character)
	}
	visual := wire.Frame{Opcode: wire.OpItemUseVisual,
		Payload: wire.NewWriter(8).U32(target).U32(ref.RefObjID).Payload()}
	result.Frames = append(result.Frames[:at+1], append([]wire.Frame{reference, visual}, result.Frames[at+1:]...)...)
	result.Broadcast = append(result.Broadcast, reference, visual)
}
