/*
===========================================================================

skillratiodamage.go - a hit measured on the target's maximum hit points

The pdm2 word of a skill program: the hit adds that percent of the
target's maximum hit points to the hit outcome (combat.HPRatioDamage),
where the fixed pdmg record adds its authored amount. The tag also
bypasses a defender's wall absorb (skilldata.go). In the shipped table
only the fortress structure skills author it - the towers at 14, the
fort stone at 22 - and no player row carries it, so the arm is inert
outside the fortress pulse until some other row authors the tag.

===========================================================================
*/

package enterworld

// tagRatioDamage is pdm2 {percent}: one of the wall-bypass tags the
// parameter index records (skilloffense.go).
const tagRatioDamage = 0x70646d32

/*
================
SkillRatioDamage

pdm2 {percent}: the hit outcome adds Percent of the target's maximum
hit points. The word is taken unsigned, as the native formula takes it.
================
*/
type SkillRatioDamage struct {
	Present bool
	Percent uint32
}
