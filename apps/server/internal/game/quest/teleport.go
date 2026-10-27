/*
===========================================================================

teleport.go - the quest lane's used-a-teleport event

The native teleport use handler raises subscribed quest event 0x26
(the use kind and the gate's gid) on the character once a 0x7495 move
has landed, before the success answer, and every subscribed quest
instance receives it through its event dispatch. Programmes subscribe
here with a note hook, as the kill lane's programmes do. No shipped
programme subscribes yet: the compiled objective kinds are talk,
collect, kill, parallel and delivery, and no registered programme
gates on a teleport. The raise itself is the contract: delivering the
event is what lets a future programme subscribe without another lane
change.

===========================================================================
*/
package quest

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TeleportUpdater

The action lane's door for the used-a-teleport event: runs once per
successful 0x7495 use, after the move, with the use kind and the
gate's gid.
================
*/
func (rt *Runtime) TeleportUpdater() func(*enterworld.Character, uint8, uint32) []wire.Frame {
	return func(character *enterworld.Character, kind uint8, npcGID uint32) []wire.Frame {
		if character == nil || character.DeletePending {
			return nil
		}
		return nil
	}
}
