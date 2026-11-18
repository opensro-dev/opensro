/*
===========================================================================

caravan_hire.go - the port-only caravan hire window's opcodes

0x3FC3 (S->C) opens or refreshes the hire window: a JSON body the
gameworld's caravan wiring builds. 0x7FC3 (C->S) is the window's request:

	u8 operation (0 refresh, 1 hire), u8 stars (a Trader's goods rating,
	0 for the others), then the agent's name bytes (at most 64)

Both were the next free pair after the arena's 0x3FCA/0x7FCA block start;
server and client were searched for them. A client that predates them
never sends 0x7FC3 and drops 0x3FC3.

===========================================================================
*/
package action

import "opensro.online/server/internal/game/enterworld"

const (
	OpCaravanHire        uint16 = 0x3fc3
	OpCaravanHireRequest uint16 = 0x7fc3
)

/*
================
HandleCaravanHire
================
*/
func (rt *Runtime) HandleCaravanHire(division string, c *enterworld.Character, payload []byte) OpResult {
	if rt.CaravanHire == nil || len(payload) < 2 || payload[0] > 1 || len(payload) > 2+64 {
		return OpResult{DiagnosticRefusal: "caravan hire request refused"}
	}
	return OpResult{Frames: rt.CaravanHire(division, c, payload)}
}
