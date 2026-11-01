/*
===========================================================================

denreturn.go - the Bandit Den return scroll's countdown rules

Port-only, not native (owner decision 2026-10-14). The original's Bandit
Den Return Scroll (ITEM_ETC_SCROLL_RETURN_THIEFDEN_01) counts down its
Param1, 300000 ms, in teleport mode 2 before the thief lands at the den
gate. With SRO_FAST_DEN_RETURN on, the countdown takes half as long. The
server publishes the rule at world entry (enterworld Deps FastDenReturn) so
the client's delay bar (return-scroll.ts) halves with it. Off, the default
here, is native; the GameWorld's Nomad job turns it on.

Port-only, not native (owner decision 2026-10-14, for the balance advisor):
with SRO_DEN_NO_TRANSPORT_SUMMON on, no transport can be summoned during
that countdown. Natively the item use refuses a transport only in teleport mode
1, so a thief could leave stolen goods on the ground, start the countdown
and call a transport to load them just before it ended. Off is native.

===========================================================================
*/
package action

import (
	"os"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

// EnvFastDenReturn turns the halved countdown on ("on", "1", "true").
const EnvFastDenReturn = "SRO_FAST_DEN_RETURN"

// denReturnSpeedup divides the Den scroll's native countdown.
const denReturnSpeedup = 2

// EnvDenNoTransportSummon refuses a transport during the Den countdown
// ("on", "1", "true").
const EnvDenNoTransportSummon = "SRO_DEN_NO_TRANSPORT_SUMMON"

/*
================
DenNoTransportSummonFromEnv
================
*/
func DenNoTransportSummonFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvDenNoTransportSummon))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
denTransportRefused

Whether the port-only rule refuses a transport summon: only a transport,
only while the character's pending return cast is the Bandit Den scroll's
(keyed on the scroll, not on its teleport mode). The native modes 1 and 3
are refused before this (teleportBlocks).
================
*/
func (rt *Runtime) denTransportRefused(division string, c *enterworld.Character, transport bool) bool {
	if !rt.DenNoTransportSummon || !transport {
		return false
	}
	pending, ok := rt.returnCasts.Load(simulation.WorldKey(division, c.Name))
	return ok && pending.(pendingReturn).thiefDen
}

/*
================
FastDenReturnFromEnv
================
*/
func FastDenReturnFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvFastDenReturn))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
denReturnDuration

The Den scroll's countdown under the shard's rule: native, or halved.
================
*/
func denReturnDuration(native int64, fast bool) int64 {
	if !fast {
		return native
	}
	return native / denReturnSpeedup
}
