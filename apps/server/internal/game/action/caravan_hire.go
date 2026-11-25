/*
===========================================================================

caravan_hire.go - the port-only caravan hire window's opcodes

0x3FC3 (S->C) opens or refreshes the hire window: a JSON body the
gameworld's caravan wiring builds. 0x7FC3 (C->S) is the window's request:

	u8 operation (0 refresh, 1 hire, 2 lead), u8 stars (a Trader's goods
	rating, 0 for the others), then the agent's name bytes (at most 64);
	lead names who leads the sender's hired Traders (itself or one of them)

0x3FC4 (S->C) is that lead state, pushed when it changes: JSON
{version, leader, candidates: [{name, display}]}, the list empty once
the hirer has no hired Trader.

0x3FC5 (S->C) follows an NPC select result when that NPC hires caravan
agents: u32 npc gid, then the hire row's label bytes. The client lists
that row first in the NPC's menu; its click sends 0x7FC3 operation 0.

0x3FC3/0x7FC3 were the next free pair after the arena's 0x3FCA/0x7FCA block start;
server and client were searched for them. A client that predates them
never sends 0x7FC3 and drops 0x3FC3.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

const (
	OpCaravanHire        uint16 = 0x3fc3
	OpCaravanHireRequest uint16 = 0x7fc3
	OpCaravanLead        uint16 = 0x3fc4
	OpCaravanHireNpc     uint16 = 0x3fc5

	CaravanHireOpLead = 2
)

// CaravanHireNpcFrame tells the client the NPC gid offers the hire row label.
func CaravanHireNpcFrame(gid uint32, label string) wire.Frame {
	w := wire.NewWriter(4 + len(label)).U32(gid)
	return wire.Frame{Opcode: OpCaravanHireNpc, Payload: append(w.Payload(), label...)}
}

/*
================
HandleCaravanHire
================
*/
func (rt *Runtime) HandleCaravanHire(division string, c *enterworld.Character, payload []byte) OpResult {
	if rt.CaravanHire == nil || len(payload) < 2 || payload[0] > CaravanHireOpLead || len(payload) > 2+64 {
		return OpResult{DiagnosticRefusal: "caravan hire request refused"}
	}
	return OpResult{Frames: rt.CaravanHire(division, c, payload)}
}
