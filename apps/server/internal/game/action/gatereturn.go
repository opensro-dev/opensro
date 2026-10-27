/*
===========================================================================

gatereturn.go - a teleport's "Teleport to return point" row

A teleport whose teleportdata row sets CanGotoResurrectPos (column 11)
holds service 9 (the reference server's teleport row reader); in v1.150
that is the Bandit Den gate alone, which has no link out and so no
destination row. Its
select word carries 0x100, the talk window draws row 0x10
(CIFNPCTalk_BuildMenuFromCapabilities 5D9100, UIIT_CTL_TELEPORT_TO_RESURRECT_POS)
and the click sends 0x7495 [u32 gid][u8 1] (CIFNpcTalk_ExecuteMenuAction
5DA1B0 case 0x10, NetClient_Send7495 6FEF10).

The reference server's teleport use handler runs, for every type: a
teleport already under way 0x1C14, the hit range, the teleport service
test (8, 9, 0x1C or 0x1E, else 3). Case 1 then refuses only a summoned
transport (0x1C10) and runs the shared town teleport with no revival: the
appointed town recall point, the same place a return scroll lands. No fee,
no quest mask, no level test.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
)

const (
	// gateTownReturn is the 0x7495 type row 0x10 sends.
	gateTownReturn uint8 = 1
	// errCodeTeleportState is 0x1C14: the player is already teleporting.
	errCodeTeleportState uint8 = 0x14
)

/*
================
handleGateTownReturn

0x7495 type 1 from the selected NPC: the native case 1 move to the
appointed return point.
================
*/
func (rt *Runtime) handleGateTownReturn(division string, c *enterworld.Character, gid uint32) OpResult {
	if c == nil {
		return portalFailure(2)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if selected, ok := rt.Selected.Get(division, c.Name); !ok || selected != gid {
		return portalFailure(2)
	}
	npc, ok := rt.npcForCurrentViewer(division, c, gid)
	if !ok {
		return portalFailure(2)
	}
	if c.NativeTeleportMode != 0 {
		return portalFailure(errCodeTeleportState)
	}
	if !rt.npcWithinHitRange(division, c, npc) {
		return portalFailure(hitRangeTooFar)
	}
	if !hasTeleportService(npc) {
		return portalFailure(errCodeTeleportService)
	}
	// The town teleport's transport rule, which the port-only
	// SRO_JOB_GOODS_TELEPORT lifts for return scrolls too.
	if rt.townTravelTransportRefused(c) {
		return portalFailure(errCodeGuideTransport)
	}
	point := rt.appointedRebirth(division, c)
	return rt.commitGateTravel(gateTravel{division: division, character: c, destination: point.spawn, world: point.world, reason: "gate-town-return"}, func() (int64, OpResult, bool) {
		if c.DeletePending || !enterworld.CharacterAlive(c) || c.NativeTeleportMode != 0 {
			return 0, portalFailure(2), false
		}
		return 0, OpResult{}, true
	})
}
