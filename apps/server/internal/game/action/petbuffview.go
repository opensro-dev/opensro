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
		tailwindFrame(gid, tail),
		{Opcode: 0x3fb9, Payload: guidance[:]},
		wolfHowlFrame(gid, howl, now),
	}
}

/*
================
tailwindFrame

The one builder of the public Tailwind frame (tailwindOpcode): recipient
u32, book u8, stacks u8, speed percent x10 u16. The live publish
(publishTailwind) and the selection refresh above both send it.
================
*/
func tailwindFrame(gid uint32, row tailwindCharge) wire.Frame {
	return wire.Frame{Opcode: tailwindOpcode, Payload: wire.NewWriter(8).U32(gid).U8(row.book).U8(row.stacks).
		U16(uint16(row.percent * float64(row.stacks) * 10)).Payload()}
}

/*
================
wolfHowlFrame

The one builder of the public Howl frame (0x3FBD): recipient u32, book u8,
speed percent x10 u16, remaining milliseconds u32. The live publish
(wolfHowlFrames) and the selection refresh above both send it.
================
*/
func wolfHowlFrame(gid uint32, howl wolfHowl, now int64) wire.Frame {
	return wire.Frame{Opcode: 0x3fbd, Payload: wire.NewWriter(11).U32(gid).U8(howl.book).
		U16(uint16(howl.percent * 10)).U32(uint32(max(int64(0), howl.until-now))).Payload()}
}
