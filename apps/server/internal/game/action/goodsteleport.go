/*
===========================================================================

goodsteleport.go - town teleport with a trade transport out

Port-only, not native (owner decision 2026-10-15, at the balance advisor's
request for testing). Natively a summoned transport refuses town travel:
a town gate's link carries Restrict kind 2 (CRefTeleportLink_CheckState
513A70: 0x1C10, the client's 0x10), a return scroll refuses it (4A0380:
0x5E) and the guide's reverse return refuses it (4F2B50 case 5: 0x10).
With SRO_JOB_GOODS_TELEPORT on, those three let the trader through and
the transport travels with them, as it does on a ferry
(relocateReturningPet). A transport standing too far from the gate still
cannot come along (4F2B50's range rule, 0x13), and the premium limited
commands keep their native refusal, which the client applies itself.

Off, the default here, is native. The GameWorld's Nomad job turns it on.

===========================================================================
*/
package action

import (
	"os"
	"strings"

	"opensro.online/server/internal/game/enterworld"
)

// EnvJobGoodsTeleport turns the rule on ("on", "1", "true").
const EnvJobGoodsTeleport = "SRO_JOB_GOODS_TELEPORT"

/*
================
JobGoodsTeleportFromEnv
================
*/
func JobGoodsTeleportFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvJobGoodsTeleport))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
townTravelTransportRefused

Whether a summoned transport refuses the player's town teleport (a return
scroll or the reverse return): natively whenever one is out, never with
the port-only rule on.
================
*/
func (rt *Runtime) townTravelTransportRefused(c *enterworld.Character) bool {
	return !rt.JobGoodsTeleport && rt.hasSummonedTransportCOS(c)
}
