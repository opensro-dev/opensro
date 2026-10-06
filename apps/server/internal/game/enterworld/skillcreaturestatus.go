/*
===========================================================================

skillcreaturestatus.go - damage-free status programs on creature default skills

A creature does not pass the player learn/equipment compiler. Its complete
program still must be known before AI may select it; matching a status tag
alone is insufficient. The Cold guild soldier authors an EFR cone (shape 4).

===========================================================================
*/
package enterworld

import "opensro.online/server/internal/game/abnormal"

/*
================
compileCreatureStatusCast

58E5F0 produces a successful zero-damage impact without att; 590680 still
rolls each authored status. Area shape validation remains in ActionArea.
================
*/
func compileCreatureStatusCast(fields []string, row SkillRow) bool {
	if len(fields) != 118 || fields[0] != "1" || fields[8] != "2" || row.Attack.Present ||
		!row.Abnormal.Present() || !row.TargetRequired || !row.TimingPinned || !row.Consumption.Pinned ||
		!row.ActionRangePinned || !row.ActionCastingTimePinned || !row.ActionDurationPinned ||
		row.ActionDurationMs == 0 || row.ChainSub || row.ChainNext != 0 {
		return false
	}
	program, err := CompileSkillProgram(fields)
	if err != nil {
		return false
	}
	seen := make(map[uint32]bool)
	count := 0
	for i := 0; i < program.Len(); i++ {
		op := program.Instruction(i)
		if seen[op.Tag] {
			return false
		}
		seen[op.Tag] = true
		if _, known := abnormal.SourceIndex(op.Tag); known {
			count++
			continue
		}
		if op.Tag != tagEfr || row.ActionArea.Shape == 0 || row.ActionArea.Select != statusCastSelect {
			return false
		}
	}
	return count > 0
}
