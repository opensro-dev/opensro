/*
===========================================================================

denreturn.go - the Bandit Den return scroll's halved countdown

Port-only, not native (owner decision 2026-10-14). The original's Bandit
Den Return Scroll (ITEM_ETC_SCROLL_RETURN_THIEFDEN_01) counts down its
Param1, 300000 ms, in teleport mode 2 before the thief lands at the den
gate. With SRO_FAST_DEN_RETURN on, the countdown takes half as long. The
server publishes the rule at world entry (enterworld Deps FastDenReturn) so
the client's delay bar (return-scroll.ts) halves with it. Off, the default
here, is native; the GameWorld's Nomad job turns it on.

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
