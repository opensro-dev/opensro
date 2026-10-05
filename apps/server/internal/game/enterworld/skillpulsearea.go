/*
===========================================================================

skillpulsearea.go - self effects that strike the enemies around their owner
on a period

The Warlock's Soul Chaos (SKILL_EU_WARLOCK_SOULA_CHAOS_A and _B) is
dura(ms) puls(ms) efr(2, shape, radius, most, reduction, select) pdmg(amount)
reqi getv(SAAA): a category-3 instance on its caster. efr kind 2 is
RefSkill +0x290, which CastLifecycle_ProcessPersistent selects afresh with
TargetSelection_DispatchByShape each time puls (+0x384) has elapsed, then
strikes the victims through SkillCombat_CalculateHitOutcome (pdmg,
Formulae_CalculateFixedSkillDamage 40F5F0) and publishes them in one B0BC.

===========================================================================
*/

package enterworld

// tagPulsePeriod is puls (+0x384).
const tagPulsePeriod = 0x70756c73

/*
================
SkillPulseArea
================
*/
type SkillPulseArea struct {
	Present  bool
	PeriodMs uint32
	Area     SkillOffensiveArea
	Fixed    SkillFixedDamage
}

/*
================
compileSkillPulseArea

A self-cast category-3 program: one dura, one puls, one kind-2 efr, one
pdmg, the reqi gate and known caster getv words.
================
*/
func compileSkillPulseArea(fields []string, row SkillRow) (SkillPulseArea, bool) {
	var out SkillPulseArea
	if len(fields) != 118 || fields[0] != "1" || fields[8] != "2" || fields[68] != "3" ||
		row.ChainNext != 0 || !row.Consumption.Pinned || !row.ActionCastingTimePinned ||
		!row.ActionDurationPinned || !row.TimingPinned || row.TargetRequired ||
		row.Consumption.HP != 0 || row.Consumption.HPPercent != 0 || row.EffectDurationMs == 0 {
		return out, false
	}
	for _, column := range []int{15, 16, 17, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 56} {
		if fields[column] != "0" {
			return out, false
		}
	}
	program, err := CompileSkillProgram(fields)
	if err != nil {
		return out, false
	}
	seen := make(map[uint32]bool)
	for i := 0; i < program.Len(); i++ {
		op := program.Instruction(i)
		if seen[op.Tag] && op.Tag != tagGetv && op.Tag != tagReqi {
			return out, false
		}
		seen[op.Tag] = true
		switch op.Tag {
		case tagDura:
			if op.Arguments[0] != row.EffectDurationMs {
				return out, false
			}
		case tagPulsePeriod:
			out.PeriodMs = op.Arguments[0]
		case tagEfr:
			a := op.Arguments
			if a[0] != 2 || a[1] != 1 || a[2] == 0 || a[2] > 0xffff || a[3] == 0 || a[3] > 255 || a[4] > 100 || a[5] > 0xff {
				return out, false
			}
			out.Area = SkillOffensiveArea{Shape: uint8(a[1]), Radius: a[2], MaxTargets: uint8(a[3]),
				ReductionPercent: uint8(a[4]), Select: uint8(a[5])}
		case tagFixedDamage:
			out.Fixed.Amount = op.Arguments[0]
		case tagReqi: // row.Reqi; 59F0E0 retires the instance on unequip
		case tagGetv:
			slot, known := SkillParameterFromKey(op.Arguments[0])
			if !known {
				return out, false
			}
			out.Fixed.Power = out.Fixed.Power || slot == ParameterFixedDamagePower
		default:
			return out, false
		}
	}
	if out.PeriodMs == 0 || out.Fixed.Amount == 0 || out.Area.Radius == 0 {
		return out, false
	}
	out.Fixed.Present = true
	out.Present = true
	return out, true
}
