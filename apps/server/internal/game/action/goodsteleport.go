/*
===========================================================================

goodsteleport.go - town teleport with a trade transport out

Port-only, not native (owner decision 2026-10-15, at the balance advisor's
request for testing). Natively a summoned transport refuses town travel:
a town gate's link carries Restrict kind 2 (the teleport link's state
check: 0x1C10, the client's 0x10), a return scroll refuses it (0x5E) and
the guide's reverse return refuses it (teleport use case 5: 0x10).
With SRO_JOB_GOODS_TELEPORT on, those three let the trader through and
the transport travels with them, as it does on a ferry
(relocateReturningPet). A transport standing too far from the gate still
cannot come along (the native range rule, 0x13); a reverse return to a point
in a dungeon still refuses a transport, since no COS ever enters one
natively; and the premium limited commands keep their native refusal,
which the client applies itself.

Off, the default here, is native. The GameWorld's Nomad job turns it on.

===========================================================================
*/
package action

import (
	"os"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
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

/*
================
transportBarredFromDestination

With the rule on, a transport still may not follow its trader into a
dungeon: a reverse return to a death point there refuses it. Natively the
transport was refused earlier, so this never fires with the rule off.
================
*/
func (rt *Runtime) transportBarredFromDestination(c *enterworld.Character, destination simulation.Spawn) bool {
	return rt.JobGoodsTeleport && rt.hasSummonedTransportCOS(c) && simulation.PlaneKindOf(destination.RegionID) == simulation.PlaneCave
}
