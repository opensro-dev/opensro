/*
===========================================================================

mountedcommand.go - on-foot commands from a mounted player

The reference server's command processing refuses attack (1) and
skill (4) commands while the player is mounted on a COS (vtable +0x540)
holds, before the command record exists, so before admission and before
the instant-skill dispatch. The mount attacks through its own COS
command instead.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// mountedCommandError is v1.188's 0x4004, COS error 4. The v1.150 client
// reads one byte under notice category 0x19 (689420), where 1..4 show no
// text.
const mountedCommandError uint8 = 0x04

// mountedOnCOS is the reference server's mounted test: a ridden COS and state+0xE == 1.
func mountedOnCOS(c *enterworld.Character) bool {
	return c.ActiveCOS != nil && c.ActiveCOS.Mounted
}

/*
==================
mountedCommandRefusal

The reference server's action response with result 3: the queued command count,
which is zero while mounted since every on-foot command is refused, then
the error.
==================
*/
func mountedCommandRefusal() OpResult {
	notice := wire.NoticeActionState(0, mountedCommandError)
	return OpResult{Frames: []wire.Frame{{Opcode: wire.OpActionState, Payload: notice.Encode()}}}
}
