/*
===========================================================================

fortress_return.go - the action window's "Return to fortress" (1015)

CGInterface_ExecuteActionCommand (client 695420) case 1015 looks up the
fortress its guild occupies (GlobalDataManager_FindFortressIdByOwnerName
7E24B0) and sends 0x7025 [u32 fortress]. The GameServer
(CGObjPC_HandleSiegeReturn705D 51A5B0) answers by the PC's own guild:
it sends a guild member of the occupying guild to a revival gate of its
fortress (CRefData_FindWorldTeleportByKind kind 2), unless the owner
timed job (2, 5) still runs, and then starts that job for 600 s. The
v1.150 client reads a refusal as 0xB025 [u8 2][u8 code], notice category
0x1F (689420): 6 a job suit is worn (UIIT_MSG_FORT_PORTALSTONE_FAIL_02), 7
no occupied fortress (FAIL_03), 8 not yet (FAIL_01). The cooldown reaches
the action window as 0x3792 [u8 2][u8 5][u32 seconds]
(CNetProcessSecond_OnTimedJobState3792 766E30).

INFERENCE: 51A5B0 also refuses inside a world whose vtable +0x78 says so
(0x7C09); only instance worlds can, and v1.150 has no code for it, so a
return from a non-permanent world reads "not yet". The job-suit refusal
is the v1.150 client's FAIL_02; the v1.188 handler leaves it to the job
system's teleport gate.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
)

const (
	opFortressReturn       uint16 = 0x7025
	opFortressReturnResult uint16 = 0xb025
	opTimedJobState        uint16 = 0x3792

	fortressReturnCooldownMs = 600 * 1000

	fortressReturnRefused     uint8 = 2
	fortressReturnJobSuit     uint8 = 6
	fortressReturnNoFortress  uint8 = 7
	fortressReturnNotYet      uint8 = 8
	timedJobStateOwner        uint8 = 2
	timedJobFortressReturnSub uint8 = 5
)

/*
================
HandleFortressReturn
================
*/
func (rt *Runtime) HandleFortressReturn(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	if _, err := r.U32(); err != nil || r.Done() != nil || c == nil {
		return OpResult{}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	gate, code := rt.fortressReturnGate(division, c)
	if code != 0 {
		return OpResult{Frames: []wire.Frame{fortressReturnRefusal(code)}}
	}
	untilMs := rt.Now().UnixMilli() + fortressReturnCooldownMs
	if !rt.relocateCharacter(division, c, "fortress-return", func() (travelPoint, bool) {
		c.FortressReturnUntilMs = untilMs
		return gate, true
	}) {
		return OpResult{}
	}
	rt.PushCharacterFrames(division, c.Name, []wire.Frame{fortressReturnCooldown(fortressReturnCooldownMs / 1000)})
	return OpResult{}
}

/*
================
fortressReturnGate

The revival gate c returns to, or the 0x1F code that refuses it.
================
*/
func (rt *Runtime) fortressReturnGate(division string, c *enterworld.Character) (travelPoint, uint8) {
	if rt.jobDressed(c) {
		return travelPoint{}, fortressReturnJobSuit
	}
	if c.GuildID == nil || rt.Fortresses == nil || rt.portals == nil {
		return travelPoint{}, fortressReturnNoFortress
	}
	fortressID, ok := rt.Fortresses.OwnedFortress(division, *c.GuildID)
	if !ok {
		return travelPoint{}, fortressReturnNoFortress
	}
	if here, ok := instance.Lookup(instance.ID(domain.CharacterWorldInstance(c)).Definition()); !ok || here.NativeType != 0 {
		return travelPoint{}, fortressReturnNotYet
	}
	if rt.Now().UnixMilli() < c.FortressReturnUntilMs {
		return travelPoint{}, fortressReturnNotYet
	}
	for _, definition := range instance.Shipped() {
		if id, ok := rt.Fortresses.ForWorld(definition); ok && id == fortressID {
			if gate, ok := rt.randomGate(rt.worldGates(definition.ID, gateKindRevival)); ok {
				return gate, 0
			}
		}
	}
	return travelPoint{}, fortressReturnNoFortress
}

/*
================
fortressReturnRefusal
================
*/
func fortressReturnRefusal(code uint8) wire.Frame {
	return wire.Frame{Opcode: opFortressReturnResult, Payload: []byte{fortressReturnRefused, code}}
}

/*
================
fortressReturnCooldown

0x3792 kind 5: the action window's fortress-portal cooldown in seconds.
================
*/
func fortressReturnCooldown(seconds uint32) wire.Frame {
	return wire.Frame{Opcode: opTimedJobState, Payload: wire.NewWriter(6).U8(timedJobStateOwner).U8(timedJobFortressReturnSub).U32(seconds).Payload()}
}

/*
================
FortressReturnCooldownFrames

The cooldown a character enters the world with, sent once it stands in
it (an empty slice when none runs).
================
*/
func FortressReturnCooldownFrames(c *enterworld.Character, nowMs int64) []wire.Frame {
	if c == nil || c.FortressReturnUntilMs <= nowMs {
		return nil
	}
	return []wire.Frame{fortressReturnCooldown(uint32((c.FortressReturnUntilMs - nowMs + 999) / 1000))}
}
