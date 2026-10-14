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
that countdown. Natively 49B9F0 refuses a transport only in teleport mode
1, so a thief could leave stolen goods on the ground, start the countdown
and call a transport to load them just before it ended. Off is native.

===========================================================================
*/
package action

import (
	"os"
	"strings"
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
only in the Den countdown's teleport mode 2 (the one free countdown). The
native modes 1 and 3 are refused before this (teleportBlocks, 49B9F0).
================
*/
func denTransportRefused(mode uint8, transport, rule bool) bool {
	return rule && transport && mode == teleportModeFree
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
