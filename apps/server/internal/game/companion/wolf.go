/*
===========================================================================
wolf.go - port-only Execute thresholds shared by wolf and owner attacks.
===========================================================================
*/
package companion

/*
================
ExecutionThreshold

Retains the original first-book threshold and bleeding proficiency bonus.
================
*/
func ExecutionThreshold(skill SkillDef, level uint8, bleeding bool) float64 {
	value := skill.ValueAt(level)
	if bleeding && (level >= D.Rules.Rank2Level || skill.Family != "" && skill.Option > 1) {
		value += 5
	}
	return value
}
