/*
===========================================================================

fortressdamage.go - the fortress hit group's amount against a structure

A hit the hit-outcome routine classifies by its fortress-class target
takes a uniform draw across the attacker's physical attack power pair
instead of the lane formula: one draw, no lane split. Equal or inverted
bounds deal 0, as the native helper returns 0 unless the high bound
exceeds the low one.

===========================================================================
*/
package combat

/*
================
FortressDamage
================
*/
func FortressDamage(attacker Stats, roll Roll32767) (uint32, error) {
	low := int64(attacker.PhysicalAttackMin)
	high := int64(attacker.PhysicalAttackMax)
	if high <= low {
		return 0, nil
	}
	draw, err := roll()
	if err != nil {
		return 0, err
	}
	return uint32(low + int64(draw)%(high-low+1)), nil
}
