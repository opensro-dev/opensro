/*
===========================================================================

jobscrolls.go - a job-suited player cannot use rescue scrolls

Port-only, not native (owner request 2026-11-11). Natively a job-suited
player may read a resurrection scroll where they fell, or a reverse return
scroll back to their last recall or death point (only a siege destination
refuses the suit, 0x18E9). With SRO_JOB_NO_RESCUE_SCROLLS on, both are
refused while the suit is worn, from the bag, a teleport gate or a premium
limited use, before anything is consumed. The client shows
UIIT_MSG_STRGERR_CANNOT_BE_USED (item-use code 0x3E, v1.150 notice 318).
Off, the default, is native.

===========================================================================
*/
package action

import (
	"os"
	"strings"

	"opensro.online/server/internal/game/enterworld"
)

// EnvJobNoRescueScrolls turns the rule on ("on", "1", "true").
const EnvJobNoRescueScrolls = "SRO_JOB_NO_RESCUE_SCROLLS"

// errCodeItemCannotBeUsed is the item-use code the v1.150 client shows as
// UIIT_MSG_STRGERR_CANNOT_BE_USED.
const errCodeItemCannotBeUsed uint8 = 0x3e

/*
================
JobNoRescueScrollsFromEnv
================
*/
func JobNoRescueScrollsFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvJobNoRescueScrolls))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
jobRefusesRescueScroll

True, with the refusal in *result, when the rule is on and c wears a job
suit.
================
*/
func (rt *Runtime) jobRefusesRescueScroll(c *enterworld.Character, result *OpResult) bool {
	if !rt.JobNoRescueScrolls || !rt.jobDressed(c) {
		return false
	}
	*result = itemUseFailure(errCodeItemCannotBeUsed)
	return true
}
