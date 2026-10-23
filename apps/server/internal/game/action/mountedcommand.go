/*
===========================================================================

mountedcommand.go - on-foot commands from a mounted player

While a player rides a COS, the server refuses their on-foot attack (1)
and skill (4) commands before the command record exists, so before
admission and before the instant-skill dispatch. The mount attacks
through its own COS command instead.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// mountedCommandError is COS error 4. The v1.150 client
// reads one byte under notice category 0x19 (689420), where 1..4 show no
// text.
const mountedCommandError uint8 = 0x04

// mountedOnCOS reports an active COS the player is riding.
func mountedOnCOS(c *enterworld.Character) bool {
	return c.ActiveCOS != nil && c.ActiveCOS.Mounted
}

/*
==================
mountedCommandRefusal

The action-state answer (B074) with result 3: the queued command count,
which is zero while mounted since every on-foot command is refused, then
the error.
==================
*/
func mountedCommandRefusal() OpResult {
	notice := wire.NoticeActionState(0, mountedCommandError)
	return OpResult{Frames: []wire.Frame{{Opcode: wire.OpActionState, Payload: notice.Encode()}}}
}
