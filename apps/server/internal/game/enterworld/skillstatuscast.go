/*
===========================================================================

skillstatuscast.go - complete damage-free hostile status programs

The Wizard's Root, Mesh Root and Lightning Shock carry no att block: the
cast is one successful zero-damage record whose only consequences are the
590680 status roll and the authored aggression. Admission is by executable
shape, never by skill name.

===========================================================================
*/

package enterworld

import "opensro.online/server/internal/game/abnormal"

const (
	// tagStatusThreat is tant, authored on every damage-free status cast.
	// It has tnt2's two-word shape (flat, percent): the v1.150 rows carry
	// percent 0 and a flat word that grows with the skill level (155 at
	// Root 1, 2503 at Root 8). Without the research corpus at hand the port
	// infers tnt2 semantics; a zero-damage record adds the flat word alone.
	tagStatusThreat = 0x74616e74

	// statusCastSelect is the hostile character mask (efr +0x14) shared by
	// every admitted offense area.
	statusCastSelect = 24
)

/*
================
compileSkillStatusCast

58E5F0 emits a successful zero-damage record for a targeted program
without att, as it does for taunts. Admit a row when its complete program is
status blocks plus aggression, caster getv modifiers and an optional
primary-centered area. The owner acts on monsters, so the row must name
Enemy_M (column 29): Mana Drain authors Enemy_P only and its description
says it has no effect on monsters. Untargeted (caster-centered) rows stay
refused until a release owner exists for an action without a primary.
================
*/
func compileSkillStatusCast(fields []string, row SkillRow) (SkillThreat, bool) {
	if len(fields) != 118 || fields[0] != "1" || fields[8] != "2" || fields[68] != "0" ||
		!row.TimingPinned || !row.Consumption.Pinned || !row.ActionRangePinned || !row.TargetRequired || !row.Targets.EnemyM ||
		row.ChainSub || row.ChainNext != 0 || row.ActionDurationMs == 0 || row.Attack.Present {
		return SkillThreat{}, false
	}
	// No projectile, no ground target, no secondary-target columns.
	for _, column := range []int{15, 16, 17, 19, 20, 24, 25, 26, 27, 28, 31, 32, 33, 56} {
		if fields[column] != "0" {
			return SkillThreat{}, false
		}
	}
	program, err := CompileSkillProgram(fields)
	if err != nil {
		return SkillThreat{}, false
	}
	var threat SkillThreat
	statuses := 0
	seen := make(map[uint32]bool)
	for i := 0; i < program.Len(); i++ {
		op := program.Instruction(i)
		if seen[op.Tag] && op.Tag != tagGetv {
			return SkillThreat{}, false
		}
		seen[op.Tag] = true
		if _, found := abnormal.SourceIndex(op.Tag); found {
			statuses++
			continue
		}
		switch op.Tag {
		case tagStatusThreat:
			threat.Present, threat.Flat, threat.Percent = true, op.Arguments[0], op.Arguments[1]
		case tagGetv: // caster modifiers (WIMD, WIRU) read at cast
			if _, known := SkillParameterFromKey(op.Arguments[0]); !known {
				return SkillThreat{}, false
			}
		case tagEfr:
			a := op.Arguments
			if a[0] != 1 || a[1] != 2 || a[2] == 0 || a[2] > 0xffff ||
				a[3] == 0 || a[3] > 255 || a[4] > 100 || a[5] != statusCastSelect {
				return SkillThreat{}, false
			}
			threat.Area = SkillOffensiveArea{Shape: uint8(a[1]), Radius: a[2], MaxTargets: uint8(a[3]),
				ReductionPercent: uint8(a[4]), Select: statusCastSelect}
		default:
			return SkillThreat{}, false
		}
	}
	if statuses == 0 || !row.Abnormal.Present() {
		return SkillThreat{}, false
	}
	return threat, true
}
