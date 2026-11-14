/*
===========================================================================

ignoredefense.go - MATTR_AVATAR_MDIA ("Ignore Monster Defense") chance

The reference server's hit-outcome routine reads parameter 0xBC only for a
player attacker, and only against a non-player target (monster or COS).

===========================================================================
*/

package combat

// ignoreDefenseParameter is ParamKeeper parameter 0xBC (188), populated flat
// from equipped avatar MATTR_AVATAR_MDIA magic options.
const ignoreDefenseParameter uint16 = 0xbc

/*
================
IgnoreDefenseChance

Returns attacker parameter 0xBC as a float when the attacker is a player
snapshot (has a parameter graph) and the defender is not (no graph), and 0
otherwise.
================
*/
func IgnoreDefenseChance(attacker, defender Stats) float64 {
	if attacker.graph == nil || defender.graph != nil {
		return 0
	}
	chance, ok := attacker.Param(ignoreDefenseParameter)
	if !ok {
		return 0
	}
	return float64(chance)
}
