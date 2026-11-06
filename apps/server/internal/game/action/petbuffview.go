/*
===========================================================================
petbuffview.go - port-only public recipient buffs for a selected player

The private training profile and equipment never leave the pet owner.
===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/item/wire"
)

/*
================
petRecipientBuffFrames

Refresh selection after entering sight, even when an unchanged stack has not broadcast.
================
*/
func (rt *Runtime) petRecipientBuffFrames(division string, gid uint32, now int64) []wire.Frame {
	if !rt.AttackPetExpansion {
		return nil
	}
	c := rt.characterSnapshotCast(division, rt.findCharacterByGid(division, gid))
	if c == nil {
		return nil
	}
	tail := rt.tailwindView(division, gid, now)
	guidance := rt.crowPrecisionView(division, gid, now)
	howl := rt.wolfHowlFor(division, c)
	return []wire.Frame{
		{Opcode: tailwindOpcode, Payload: wire.NewWriter(8).U32(gid).U8(tail.book).U8(tail.stacks).U16(uint16(tail.percent * float64(tail.stacks) * 10)).Payload()},
		{Opcode: 0x3fb9, Payload: guidance[:]},
		{Opcode: 0x3fbd, Payload: wire.NewWriter(11).U32(gid).U8(howl.book).U16(uint16(howl.percent * 10)).U32(uint32(max(int64(0), howl.until-now))).Payload()},
	}
}
