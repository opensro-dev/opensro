/*
===========================================================================

ratiodamage.go - damage measured on the target's maximum hit points

===========================================================================
*/

package combat

import "opensro.online/server/internal/game/item/wire"

// ratioDamageDivisor turns the authored percent into a share.
const ratioDamageDivisor = 100

/*
================
HPRatioDamage

The hit outcome's ratio arm: the target's maximum hit points times
percent over 100, truncated. The reference server's ratio routine
multiplies in the x87 unit and converts under a control word or'd with
0xC00, the round-toward-zero mode, so 14 % of 999 is 139, not 140. The product of two integers is exact there, which the integer
form below reproduces without a float. From there the amount is an
ordinary damage record: the wire ceiling here, then the debits and caps
every hit takes.
================
*/
func HPRatioDamage(percent uint32, maxHP int64) uint32 {
	if percent == 0 || maxHP <= 0 {
		return 0
	}
	damage := uint64(percent) * uint64(maxHP) / ratioDamageDivisor
	return uint32(min(damage, uint64(wire.MaxSkillActionDamage)))
}
