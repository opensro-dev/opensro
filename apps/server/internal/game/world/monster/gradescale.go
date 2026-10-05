/*
===========================================================================

gradescale.go - a graded monster's larger body

CGObjMob_SpawnBase (4C10C0) stores the grade (+0x1CD8) and calls
CGObjMob_RefreshGradeScale1CDC (4C1550), which keeps one scale at +0x1CDC:
a champion 1.5, a giant 3, an elite 1.7, every other grade 1. Two owners
read it. CGObjMob_ApplyGradeSpeedScale (vtable +0x35C, 4C1690) multiplies
the walk and run speeds the reference seeds (parameters 0x17 and 0x18), so
a larger body covers ground at its stride; CGObjMob_GetBodyRadius (vtable
+0x560, 4C1BF0) truncates BCRadius times the scale, the radius every
combat spacing reads.

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

4C1550 on the grade's low nibble. An unknown grade dumps and takes 1.
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

4C1690: the seeded speed times the scale on the x87 stack, stored float32.
================
*/
func (i Instance) gradeSpeed(base float64) float64 {
	return float64(float32(float64(GradeScale(i.Rarity())) * float64(float32(base))))
}

/*
================
BodyRadius

4C1BF0: ftol(BCRadius * scale).
================
*/
func (i Instance) BodyRadius() float64 {
	return math.Trunc(float64(float32(i.Ref.BodyRadius)) * float64(GradeScale(i.Rarity())))
}
