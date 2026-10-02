/*
===========================================================================

skilltargets.go - a skill's target columns (22..33)

===========================================================================
*/

package enterworld

/*
==================
SkillTargets

SkillTargets preserves the client table's target columns 22..33.
Skill_ValidateTargetPermissions (58D7A0) reads these bytes. +0x97 is
column 25 TargetType_Building; +0x98 is column 26 TargetGroup_Self.
==================
*/
type SkillTargets struct {
	Present                           bool
	Required                          bool
	Animal, Land, Building            bool
	Self, Ally, Party                 bool
	EnemyM, EnemyP, Neutral, DontCare bool
	// DeadBody is column 33, TargetEtc_SelectDeadBody: the target is a
	// corpse (Monster Mask).
	DeadBody bool
}

// skillTargetsFromColumns reads columns 22..32. A cell other than 0 or 1
// leaves that byte clear, matching the Target_Required parser.
/*
================
skillTargetsFromColumns
================
*/
func skillTargetsFromColumns(fields []string) SkillTargets {
	var out SkillTargets
	if len(fields) <= 32 {
		return out
	}
	bits := []*bool{
		&out.Required, &out.Animal, &out.Land, &out.Building,
		&out.Self, &out.Ally, &out.Party,
		&out.EnemyM, &out.EnemyP, &out.Neutral, &out.DontCare,
	}
	for i, bit := range bits {
		value, ok := textdataInt(fields[22+i])
		if ok && (value == 0 || value == 1) {
			*bit = value == 1
		}
	}
	if len(fields) > 33 {
		if value, ok := textdataInt(fields[33]); ok && value == 1 {
			out.DeadBody = true
		}
	}
	out.Present = true
	return out
}
