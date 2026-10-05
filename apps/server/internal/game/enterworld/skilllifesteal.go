/*
===========================================================================

skilllifesteal.go - hits that take HP from the target and give it to the
caster

The Warlock's Life Drain (SKILL_EU_WARLOCK_BLOODA_LIFEDRAIN_A and _B) is
lfst(amount) mwhs(percent) [efr] ds(...) getv(BSHP), cast on an enemy with
no att block. SkillCombat_CalculateHitOutcome (58F4B5) makes the hit's
damage Formulae_CalculateSkillHeal (40F750): the lfst amount plus the
caster's BSHP and the mwhs share of its magical weapon, cut by the target's
level advantage, held at the target's HP, scaled by the victim's area
percent; the caster recovers what was taken. The status blocks roll as on
any hit. Admission is by executable shape, never by name.

===========================================================================
*/

package enterworld

import "opensro.online/server/internal/game/abnormal"

const (
	// tagLifeSteal is lfst (RefSkill +0x400), tagWeaponLifeSteal mwhs
	// (+0x404): the percent of the magical weapon added to the base.
	tagLifeSteal       = 0x6c667374
	tagWeaponLifeSteal = 0x6d776873
)

/*
================
SkillLifeSteal
================
*/
type SkillLifeSteal struct {
	Present       bool
	Amount        uint32
	WeaponPercent uint32
	// Power is getv BSHP (+0x534): the caster's ParameterLifeStealPower
	// joins the base.
	Power bool
}

/*
================
compileSkillLifeSteal

A handler-0 program (instant or prepared) with no att on a required enemy target:
one lfst, at most one mwhs and one kind-1 efr, statuses, and known caster
getv words.
================
*/
func compileSkillLifeSteal(fields []string, row SkillRow) (SkillLifeSteal, SkillOffensiveArea, bool) {
	var out SkillLifeSteal
	var area SkillOffensiveArea
	if len(fields) != 118 || fields[0] != "1" || fields[8] != "2" || fields[68] != "0" ||
		!row.TimingPinned || !row.Consumption.Pinned || !row.ActionRangePinned || row.ActionRange == 0 ||
		row.ChainSub || row.ChainNext != 0 || !row.ActionCastingTimePinned || row.ActionDurationMs == 0 ||
		row.Attack.Present || !row.TargetRequired || !row.Targets.EnemyM {
		return out, area, false
	}
	for _, column := range []int{15, 16, 17, 19, 20, 24, 25, 26, 27, 28, 31, 32, 33, 56} {
		if fields[column] != "0" {
			return out, area, false
		}
	}
	program, err := CompileSkillProgram(fields)
	if err != nil {
		return out, area, false
	}
	seen := make(map[uint32]bool)
	for i := 0; i < program.Len(); i++ {
		op := program.Instruction(i)
		if seen[op.Tag] && op.Tag != tagGetv {
			return out, area, false
		}
		seen[op.Tag] = true
		if _, status := abnormal.SourceIndex(op.Tag); status {
			continue
		}
		switch op.Tag {
		case tagLifeSteal:
			out.Amount = op.Arguments[0]
		case tagWeaponLifeSteal:
			out.WeaponPercent = op.Arguments[0]
		case tagEfr:
			a := op.Arguments
			if a[0] != 1 || a[1] < 1 || a[1] > 2 || a[2] == 0 || a[2] > 0xffff ||
				a[3] == 0 || a[3] > 255 || a[4] > 100 || a[5] > 0xff {
				return out, area, false
			}
			area = SkillOffensiveArea{Shape: uint8(a[1]), Radius: a[2], MaxTargets: uint8(a[3]),
				ReductionPercent: uint8(a[4]), Select: uint8(a[5])}
		case tagGetv:
			slot, known := SkillParameterFromKey(op.Arguments[0])
			if !known {
				return out, area, false
			}
			out.Power = out.Power || slot == ParameterLifeStealPower
		default:
			return out, area, false
		}
	}
	if out.Amount == 0 {
		return out, area, false
	}
	out.Present = true
	return out, area, true
}
