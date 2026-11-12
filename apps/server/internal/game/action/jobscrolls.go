/*
===========================================================================

jobscrolls.go - a job-suited player may be barred from rescue scrolls

Port-only, not native (owner request 2026-11-11). Natively a job-suited
player may read a resurrection scroll where they fell, or a reverse return
scroll back to their last recall or death point (only a siege destination
refuses the suit, 0x18E9). Two live tunables bar each scroll on its own
while the suit is worn:

  - SRO_JOB_BLOCK_RESURRECTION_SCROLL: the bag and the premium limited use;
  - SRO_JOB_BLOCK_REVERSE_RETURN_SCROLL: the bag, a teleport gate and the
    premium limited use.

Each is read at the use from tunables.Current(), so the staff console turns
it on or off without a restart. A refused use consumes nothing and answers
item-use code 0x3E, which the v1.150 client shows as
UIIT_MSG_STRGERR_CANNOT_BE_USED (notice 318). 0, the default, is native.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/tunables"
)

// errCodeItemCannotBeUsed is the item-use code the v1.150 client shows as
// UIIT_MSG_STRGERR_CANNOT_BE_USED.
const errCodeItemCannotBeUsed uint8 = 0x3e

/*
================
jobRefusesScroll

True, with the refusal in *result, when the tunable key is on and c
wears a job suit.
================
*/
func (rt *Runtime) jobRefusesScroll(key string, c *enterworld.Character, result *OpResult) bool {
	if tunables.Current().Int(key) == 0 || !rt.jobDressed(c) {
		return false
	}
	*result = itemUseFailure(errCodeItemCannotBeUsed)
	return true
}
