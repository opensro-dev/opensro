/*
===========================================================================

levelscaled.go - the monster-shaped attacker's level-scaled hit damage

The reference server's hit-outcome routine gives a player in a monster's
shape (transform mode 1) a hit group of its own: a plain record that is
never a critical, with no lanes and no defence, whose damage is five
times the attacker's level plus a uniform draw below the same span.
One draw per hit; the action runtime owns when this replaces the lanes.

===========================================================================
*/

package combat

/*
================
LevelScaledDamage

5L + rand() % (5L) at attacker level L: the result spans 5L .. 10L-1.
A zero level cannot occur for a living attacker; it yields 0 rather
than a zero modulus.
================
*/
func LevelScaledDamage(level uint8, roll Roll32767) (uint32, error) {
	span := 5 * uint32(level)
	if span == 0 {
		return 0, nil
	}
	draw, err := roll()
	if err != nil {
		return 0, err
	}
	return span + draw%span, nil
}
