/*
===========================================================================

macro.go - the SRO Macro settings lane on the action runtime (#3907)

Port-only, not native (SRO_AUTO_MACRO). internal/game/macro owns the wire
and the rules; this file stores its answers on the character and replays
the configuration at login. Off, the default, is native: 0x7554 is
dropped unanswered and no 0x3555 is sent, exactly what a v1.150 shard does.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/macro"
)

/*
================
ConfigureMacro - called once at startup with macro.Enabled's answer.
================
*/
func (rt *Runtime) ConfigureMacro(enabled bool) {
	rt.macro = enabled
}

/*
================
HandleMacroRequest

One 0x7554 switch or save. Register installs it whatever the flag says
and the flag is read here, so ConfigureMacro and Register may run in
either order. A change the store refuses answers the failure word, so the
client keeps its pending copy instead of believing a write that did not
happen.
================
*/
func (rt *Runtime) HandleMacroRequest(_ string, c *enterworld.Character, payload []byte) OpResult {
	if !rt.macro || c == nil {
		return OpResult{}
	}
	result := macro.Apply(c.Macro, payload)
	if result.Payload == nil {
		return OpResult{DiagnosticRefusal: "macro request without an action and kind"}
	}
	if result.Changed {
		next := result.Settings
		if !rt.deps.Update(c, "macro-settings", func() bool {
			if c.DeletePending || c.Macro == next {
				return false
			}
			c.Macro = next
			return true
		}) && c.Macro != next {
			return OpResult{Frames: []wire.Frame{{Opcode: macro.OpAnswer, Payload: macro.RefusalPayload(payload)}}}
		}
	}
	return OpResult{Frames: []wire.Frame{{Opcode: macro.OpAnswer, Payload: result.Payload}}}
}

/*
================
macroEntryFrames

The 0x3555 configuration replay, once per login; nothing with the lane off.
================
*/
func (rt *Runtime) macroEntryFrames(c *enterworld.Character) []enterworld.Packet {
	if !rt.macro || c == nil {
		return nil
	}
	return []enterworld.Packet{enterworld.NewPacket(macro.OpConfig, macro.ConfigPayload(c.Macro))}
}
