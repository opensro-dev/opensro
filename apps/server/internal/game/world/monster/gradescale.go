/*
===========================================================================

gradescale.go - a graded monster's larger body

The native monster spawn stores the grade and keeps one scale for it: a
champion 1.5, a giant 3, an elite 1.7, every other grade 1. Two owners
read it. The native speed scaling multiplies the walk and run speeds the
reference seeds (parameters 0x17 and 0x18), so a larger body covers ground
at its stride; the native body radius truncates BCRadius times the scale,
the radius every combat spacing reads.

===========================================================================
*/

package monster

import "math"

const (
	gradeChampion = 1
	gradeGiant    = 4
	gradeElite    = 6
)

/*
================
GradeScale

The native scale on the grade's low nibble. An unknown grade dumps and takes 1.
================
*/
func GradeScale(rarity uint8) float32 {
	switch rarity & 0x0f {
	case gradeChampion:
		return 1.5
	case gradeGiant:
		return 3
	case gradeElite:
		return 1.70000005
	}
	return 1
}

/*
================
gradeSpeed

The native speed scaling: the seeded speed times the scale on the x87
stack, stored float32.
================
*/
func (i Instance) gradeSpeed(base float64) float64 {
	return float64(float32(float64(GradeScale(i.Rarity())) * float64(float32(base))))
}

/*
================
BodyRadius

The native body radius: ftol(BCRadius * scale).
================
*/
func (i Instance) BodyRadius() float64 {
	return math.Trunc(float64(float32(i.Ref.BodyRadius)) * float64(GradeScale(i.Rarity())))
}
