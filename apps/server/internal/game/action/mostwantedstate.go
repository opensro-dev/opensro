/*
===========================================================================

mostwantedstate.go - the wanted-state frame's senders

Port-only, not native (#1849; the rule is owner decision 2026-10-14). With
SRO_MOST_WANTED on, the streak holder is sent its own wanted state - the
level, the kills toward the next, the dressed job and the level's damage
percent - so the client can draw it as a buff-board row: at world entry
(enterworld re-raises it beside the other board rows), on every counted
kill, when the job suit goes on over a saved streak, and once more,
cleared, when the streak ends. The frame is display only; the bonuses
stay mostwanted.go's stat projection, so the row always shows what
applies. Off, the server's default, no frame is ever sent, as today.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
mostWantedStateOf

The wanted state a character's frame reports. A cleared or absent streak
reads level 0 with no progress; the job is the one currently dressed, so a
suit removed before the frame is built reads unsuited.
================
*/
func mostWantedStateOf(c *enterworld.Character) wire.MostWantedState {
	state := wire.MostWantedState{KillsPerLevel: uint8(mostWantedKillsPerLevel)}
	if c == nil {
		return state
	}
	level := mostWantedLevel(c)
	state.Level = uint8(level)
	state.KillsIntoLevel = uint8(c.MostWanted.KillsIntoLevel())
	state.Job = enterworld.DressedJob(c)
	state.DamagePercent = uint8(mostWantedDamagePercent(level))
	return state
}

/*
================
Runtime.mostWantedStateFrame

The holder's wanted-state frame, or nothing with the rule off.
================
*/
func (rt *Runtime) mostWantedStateFrame(c *enterworld.Character) []wire.Frame {
	if !rt.MostWanted || c == nil {
		return nil
	}
	return []wire.Frame{{Opcode: wire.OpMostWantedState, Payload: wire.EncodeMostWantedState(mostWantedStateOf(c))}}
}
