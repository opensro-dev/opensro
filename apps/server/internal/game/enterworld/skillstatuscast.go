/*
===========================================================================

skillstatuscast.go - complete damage-free hostile status programs

The Wizard's Root, Mesh Root and Lightning Shock, the Warrior's Axis
Quiver and the Rogue's Poison Field carry no att block: the cast is one
successful zero-damage record whose only consequences are the 590680
status roll and the authored aggression. Admission is by executable
shape, never by skill name.

===========================================================================
*/

package enterworld

import "opensro.online/server/internal/game/abnormal"

const (
	// tagStatusThreat is tant, authored on every damage-free status cast.
	// 58914D binds +3C8; 590402..590463 applies its percentage, then
	// 5904CF adds its flat word. It takes precedence over tnt2 (+3CC).
	// Shipped status casts author zero percent, leaving only the flat word.
	tagStatusThreat = 0x74616e74

	// statusCastSelect is the hostile character mask (efr +0x14) shared by
	// every admitted offense area.
	statusCastSelect = 24

	// statusCastContinueColumn is ContinueBasicAttack (skilldata column 19).
	// Axis Quiver authors 1; the combat intent resumes the basic attack
	// after the skill generically (basicattack.go, 4AEC9E..4AECB3), so the
	// flag needs nothing from this owner. Any other value is not a flag.
	statusCastContinueColumn = 19
)

/*
================
compileSkillStatusCast

58E5F0 emits a successful zero-damage record for a program without att,
as it does for taunts. Admit a row when its complete program is status
blocks plus aggression, caster getv modifiers, reqi equipment pairs and
an optional primary-centered area. Aggression is tant or tnt2 (Axis
Quiver); with both, tant wins (5903F6). 58E5F0 admits tnt2 without att and
emits a successful zero-damage record (compileSkillTaunt). reqi pairs are stored
on row.Reqi by noteParameterIndex and enforced before dispatch by 58D480
(action.skillEquipmentRefusal, 0x300D); Poison Field authors two of them,
so reqi, like getv, may repeat. A targeted row acts on monsters, so it
must name Enemy_M (column 29): Mana Drain authors Enemy_P only and its
description says it has no effect on monsters. An untargeted row selects
its hostile victims around the caster (efr shape 1, select 24) and leaves
the target columns empty (Lightning Impact).
================
*/
func compileSkillStatusCast(fields []string, row SkillRow) (SkillThreat, bool) {
	if len(fields) != 118 || fields[0] != "1" || fields[8] != "2" || fields[68] != "0" ||
		!row.TimingPinned || !row.Consumption.Pinned || !row.ActionRangePinned || row.TargetRequired && !row.Targets.EnemyM ||
		row.ChainSub || row.ChainNext != 0 || row.ActionDurationMs == 0 || row.Attack.Present {
		return SkillThreat{}, false
	}
	// No projectile, no ground target, no secondary-target columns.
	for _, column := range []int{15, 16, 17, 20, 24, 25, 26, 27, 28, 31, 32, 33, 56} {
		if fields[column] != "0" {
			return SkillThreat{}, false
		}
	}
	if continueAttack := fields[statusCastContinueColumn]; continueAttack != "0" && continueAttack != "1" {
		return SkillThreat{}, false
	}
	program, err := CompileSkillProgram(fields)
	if err != nil {
		return SkillThreat{}, false
	}
	var threat SkillThreat
	statuses := 0
	seen := make(map[uint32]bool)
	statusThreat := false
	for i := 0; i < program.Len(); i++ {
		op := program.Instruction(i)
		if seen[op.Tag] && op.Tag != tagGetv && op.Tag != tagReqi {
			return SkillThreat{}, false
		}
		seen[op.Tag] = true
		if _, found := abnormal.SourceIndex(op.Tag); found {
			statuses++
			continue
		}
		switch op.Tag {
		case tagStatusThreat:
			// 5903F6: a present tant takes the record's aggression and the
			// tnt2 block is skipped, in whichever order the row authors them.
			threat.Present, threat.Flat, threat.Percent = true, op.Arguments[0], op.Arguments[1]
			statusThreat = true
		case tagThreat:
			if !statusThreat {
				threat.Present, threat.Flat, threat.Percent = true, op.Arguments[0], op.Arguments[1]
			}
		case tagReqi: // row.Reqi; 58D480 admits before dispatch
		case tagGetv: // caster getv modifiers (WIMD, WIRU, RPDU, RPTU) read at cast
			if _, known := SkillParameterFromKey(op.Arguments[0]); !known {
				return SkillThreat{}, false
			}
		case tagEfr:
			a := op.Arguments
			// A targeted row centres on its primary (shape 2); an untargeted
			// row centres on the caster (shape 1, Lightning Impact).
			wantShape := uint32(2)
			if !row.TargetRequired {
				wantShape = 1
			}
			if a[0] != 1 || a[1] != wantShape || a[2] == 0 || a[2] > 0xffff ||
				a[3] == 0 || a[3] > 255 || a[4] > 100 || a[5] != statusCastSelect {
				return SkillThreat{}, false
			}
			threat.Area = SkillOffensiveArea{Shape: uint8(a[1]), Radius: a[2], MaxTargets: uint8(a[3]),
				ReductionPercent: uint8(a[4]), Select: statusCastSelect}
		default:
			return SkillThreat{}, false
		}
	}
	if statuses == 0 || !row.Abnormal.Present() || !row.TargetRequired && threat.Area.Radius == 0 {
		return SkillThreat{}, false
	}
	return threat, true
}
