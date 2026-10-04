package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
HandleVisualFlags

The full retail 0x7683 -> 0xB683 round trip. The client composes both known
bits: the options page sets the beginner mark (4BADC0) and the Action
window's Helper status toggles the helper mark (695420 action 1011).
Neither binary shows a server rule for the helper bit, so it is inferred to
be the player's own choice, accepted as sent. Above level 19 the beginner
mark is forced off, matching the native option gate and tooltip.
================
*/
func (rt *Runtime) HandleVisualFlags(
	_ string,
	character *enterworld.Character,
	payload []byte,
) OpResult {
	requested, err := wire.DecodeVisualFlagsRequest(payload)
	if err != nil || character == nil || character.DeletePending {
		return OpResult{}
	}

	var committed uint8
	applied := rt.deps.Update(character, "visual-flags", func() bool {
		// The native senders compose only the two known bits.
		if requested&^enterworld.VisualFlagsKnownMask != 0 {
			return false
		}
		level := int64(1)
		if character.Level != nil {
			level = *character.Level
		}
		committed = requested & enterworld.VisualFlagsKnownMask
		if level > enterworld.BeginnerMarkMaxLevel {
			committed &^= enterworld.VisualFlagBeginner
		}
		value := int64(committed)
		character.VisualFlags = &value
		return true
	})
	if !applied {
		return OpResult{}
	}

	frame := wire.Frame{
		Opcode:  wire.OpVisualFlagsUpdate,
		Payload: wire.EncodeVisualFlagsUpdate(enterworld.ObjectIDForCharacter(character), committed),
	}
	return OpResult{Frames: []wire.Frame{frame}, Broadcast: []wire.Frame{frame}}
}
