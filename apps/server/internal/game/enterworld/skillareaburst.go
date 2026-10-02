/*
===========================================================================

skillareaburst.go - untargeted caster-centred attack programs

The Bard's Booming Chord and Booming Wave are the only v1.150 activity-2
attacks that author att with Target_Required 0 outside the planted traps
(activity-1 imbues and buffs also author att untargeted, but are not
attacks): the caster needs no target, and the strike lands on the hostile
monsters around it.
Admission is by executable shape, never by skill name: one att, its mc
impact count, one caster-centred kind-1 efr and caster getv modifiers.

===========================================================================
*/

package enterworld

/*
================
compileSkillAreaBurst

Admit an instant untargeted attack whose victims are selected around the
caster (efr kind 1, shape 1, select 24, as the untargeted status area). The
target columns stay empty and the row carries no range, flight or link.
Every shipped row authors zero casting time, so the cast resolves at command
time like every other zero-casting-time action in this port; a positive
casting time has no admitted release owner here and refuses. Anything
beyond att, mc, efr and known getv keys refuses.
================
*/
func compileSkillAreaBurst(fields []string, row SkillRow) (SkillOffensiveArea, bool) {
	if len(fields) != 118 || fields[0] != "1" || fields[8] != "2" || fields[68] != "0" ||
		row.TargetRequired || !row.CombatPinned || !row.Attack.Present || row.Attack.ImpactCount == 0 ||
		!row.TimingPinned || !row.Consumption.Pinned || !row.ActionRangePinned ||
		row.ChainSub || row.ChainNext != 0 || row.ActionCastingTimeMs != 0 || row.ActionDurationMs == 0 ||
		row.Consumption.HP != 0 || row.Consumption.HPPercent != 0 {
		return SkillOffensiveArea{}, false
	}
	// No projectile, no range, no target columns (21..33), no linked stage.
	for _, column := range []int{15, 16, 17, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 56} {
		if fields[column] != "0" {
			return SkillOffensiveArea{}, false
		}
	}
	program, err := CompileSkillProgram(fields)
	if err != nil {
		return SkillOffensiveArea{}, false
	}
	var area SkillOffensiveArea
	seen := make(map[uint32]bool)
	for i := 0; i < program.Len(); i++ {
		op := program.Instruction(i)
		a := op.Arguments
		if seen[op.Tag] && op.Tag != tagGetv {
			return SkillOffensiveArea{}, false
		}
		seen[op.Tag] = true
		switch op.Tag {
		case uint32(skillAttackTag):
			if a[0]&(physicalAttackFlagBit|magicalAttackFlagBit) == 0 || a[1] == 0 || a[2] > a[3] || a[4] != 0 {
				return SkillOffensiveArea{}, false
			}
		case uint32(skillMultiImpactTag):
			// The impact count itself is the row parser's (ImpactCount).
		case tagEfr:
			if a[0] != 1 || a[1] != 1 || a[2] == 0 || a[2] > 0xffff ||
				a[3] == 0 || a[3] > 255 || a[4] > 100 || a[5] != statusCastSelect {
				return SkillOffensiveArea{}, false
			}
			area = SkillOffensiveArea{Shape: 1, Radius: a[2], MaxTargets: uint8(a[3]),
				ReductionPercent: uint8(a[4]), Select: statusCastSelect}
		case tagGetv: // caster modifiers (BDMD prepared cost, MUAT damage)
			if _, known := SkillParameterFromKey(a[0]); !known {
				return SkillOffensiveArea{}, false
			}
		default:
			return SkillOffensiveArea{}, false
		}
	}
	if !seen[uint32(skillAttackTag)] || area.Radius == 0 {
		return SkillOffensiveArea{}, false
	}
	return area, true
}
