/*
===========================================================================

skillcoating.go - status-only weapon imbues

The zero att payload is intentional: the coating contributes poison through
590680 without adding elemental damage. RPBU extends the owner lifetime at
5833BE; RPDU and RPTU are resolved when the weapon strikes.

===========================================================================
*/
package enterworld

const (
	tagCoatingPoison         = 0x7073
	parameterPoisonDamage    = 0x52504455
	parameterPoisonDuration  = 0x52505455
	parameterCoatingDuration = 0x52504255
)

/*
================
compilePoisonCoating

Admit the entire program without depending on a skill ID, name or rank.
Equipment predicates remain owned by the common reqi admission path.
================
*/
func compilePoisonCoating(fields []string, row SkillRow) SkillImbue {
	var empty SkillImbue
	if len(fields) != 118 || fields[0] != "1" || fields[8] != "1" || fields[68] != "3" ||
		row.ChainNext != 0 || !row.Consumption.Pinned || !row.TimingPinned ||
		row.Consumption.HP != 0 || row.Consumption.HPPercent != 0 {
		return empty
	}
	for _, column := range []int{9, 12, 13, 15, 16, 17, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 56} {
		if fields[column] != "0" {
			return empty
		}
	}
	program, err := CompileSkillProgram(fields)
	if err != nil {
		return empty
	}
	seen := make(map[uint32]bool)
	keys := make(map[uint32]bool)
	for i := 0; i < program.Len(); i++ {
		op := program.Instruction(i)
		if seen[op.Tag] && op.Tag != tagGetv && op.Tag != tagReqi {
			return empty
		}
		seen[op.Tag] = true
		a := op.Arguments
		switch op.Tag {
		case tagDura:
			if a[0] == 0 {
				return empty
			}
		case uint32(skillAttackTag):
			if a[0] != 8 || a[1] != 0 || a[2] != 0 || a[3] != 0 || a[4] != 0 {
				return empty
			}
		case tagCoatingPoison:
			if a[0] == 0 || a[0] > 65535 || a[1] > 100 {
				return empty
			}
		case tagGetv:
			if keys[a[0]] {
				return empty
			}
			if a[0] != parameterPoisonDamage && a[0] != parameterPoisonDuration && a[0] != parameterCoatingDuration {
				return empty
			}
			keys[a[0]] = true
		case tagReqi:
			if a[0] != 6 || (a[1] != 12 && a[1] != 13) {
				return empty
			}
		default:
			return empty
		}
	}
	if !seen[tagDura] || !seen[uint32(skillAttackTag)] || !seen[tagCoatingPoison] ||
		!seen[tagReqi] || len(keys) != 3 {
		return empty
	}
	return SkillImbue{Pinned: true, Poison: true}
}
