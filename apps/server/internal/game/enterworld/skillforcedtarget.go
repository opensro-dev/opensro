/*
===========================================================================

skillforcedtarget.go - timed hitm target constraints

Scorn and its area form execute the same category-three hitm program.
The marker is independent of damage, abnormal-status rolls and Warrior threat.

===========================================================================
*/
package enterworld

const tagForcedTarget = 0x6869746d

/*
================
compileForcedTarget

5838AC installs the caster GID as the recipient context's target. The
target list is player-only; Gross Scorn adds a primary-centered EFR.
================
*/
func compileForcedTarget(fields []string, row SkillRow) SkillTimedEffect {
	var out SkillTimedEffect
	if len(fields) != 118 || fields[0] != "1" || fields[8] != "2" || fields[68] != "3" ||
		!row.TimingPinned || !row.Consumption.Pinned || !row.ReplacementPinned ||
		!row.TargetRequired || !row.ActionRangePinned || row.ActionRange == 0 ||
		row.ChainSub || row.ChainNext != 0 || row.ActionCastingTimeMs != 0 || row.ActionDurationMs == 0 {
		return out
	}
	for _, column := range []int{9, 12, 15, 16, 17, 19, 20, 24, 25, 26, 27, 28, 29, 31, 32, 33, 56} {
		if fields[column] != "0" {
			return out
		}
	}
	if fields[22] != "1" || fields[23] != "1" || fields[30] != "1" {
		return out
	}
	program, err := CompileSkillProgram(fields)
	if err != nil {
		return out
	}
	seen := make(map[uint32]bool)
	for i := 0; i < program.Len(); i++ {
		op := program.Instruction(i)
		if seen[op.Tag] {
			return SkillTimedEffect{}
		}
		seen[op.Tag] = true
		switch op.Tag {
		case tagForcedTarget, tagNbuf, tagBbuf:
		case tagDura:
			if op.Arguments[0] == 0 {
				return SkillTimedEffect{}
			}
		case tagEfr:
			a := op.Arguments
			if a[0] != 1 || a[1] != 2 || a[2] == 0 || a[2] > 65535 || a[3] == 0 || a[3] > 255 || a[4] != 0 || a[5] != SelectHostile {
				return SkillTimedEffect{}
			}
			out.Area = SkillRecipientArea{Present: true, Radius: a[2], MaxTargets: a[3], Select: a[5]}
		default:
			return SkillTimedEffect{}
		}
	}
	if !seen[tagForcedTarget] || !seen[tagDura] || !seen[tagNbuf] || !seen[tagBbuf] {
		return SkillTimedEffect{}
	}
	out.Pinned, out.Targeted, out.ForcedTarget = true, true, true
	return out
}
