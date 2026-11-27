/*
===========================================================================

ratiodamage.go - damage measured on the target's maximum hit points

===========================================================================
*/

package combat

import (
	"math"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
HPRatioDamage

The hit outcome's ratio arm: round(percent / 100 * the target's
maximum hit points), computed in the native float math and rounded to
the nearest point. From there the amount is an ordinary damage record:
the wire ceiling here, then the debits and caps every hit takes.
================
*/
func HPRatioDamage(percent uint32, maxHP int64) uint32 {
	if percent == 0 || maxHP <= 0 {
		return 0
	}
	damage := math.Round(float64(percent) / 100 * float64(maxHP))
	if damage <= 0 {
		return 0
	}
	return min(uint32(damage), wire.MaxSkillActionDamage)
}
