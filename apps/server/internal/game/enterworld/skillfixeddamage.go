/*
===========================================================================

skillfixeddamage.go - fixed-damage hits whose damage feeds the caster's MP

The Bard's Tuning Noise and Tuning Sound (SKILL_EU_BARD_FORGETA_MPABSORB_A
and _B) are pdmg(amount) dmgt(0,100) getv(BDMD), cast on an enemy (columns
22, 23, 29 and 30: required, animal, Enemy_M, Enemy_P). Their description:
"You can convert the damage of enemies into your MP through the wave. This
skill disregards the defensive power of enemies." There is no att block:
the hit is the pdmg amount itself. The action owner is the ordinary
single-target offensive release (action/skillcombat.go); admission is by
executable shape, never by name.

The Cleric's Over Healing (SKILL_EU_CLERIC_BATTLEA_OVERHEAL_A) is the same
hit without the drain: pdmg(amount) cm(2,1), prepared (column 12) and cast
on an enemy. Its description: "Injures enemies with a healing power
surpassing enemies' vitality. [...] This skill disregards the defensive
power of enemies." cm(2,1) is one generated result, the count retail
starts from without any cm block (skilldata.go).

Glut Healing (SKILL_EU_CLERIC_BATTLEA_OVERHEAL_B) adds efr(1,2,50,3,35,24):
"Deals great damage to enemies in the front line [...]". Its victims are
selected and their percent falls as for every offensive area (the kind-1
efr Life Drain also authors); each takes the fixed record at its percent.

===========================================================================
*/

package enterworld

const (
	// tagFixedDamage is pdmg {amount}: the client tooltip prints the word
	// as the hit's damage. It is also one of the 589EE0 wall-bypass tags
	// noteParameterIndex records.
	tagFixedDamage = 0x70646d67

	// tagDamageTransfer is dmgt {HP percent, MP percent}.
	tagDamageTransfer = 0x646d6774

	// maxDamageTransferPercent bounds a share: more than the whole damage
	// has no meaning.
	maxDamageTransferPercent = 100
)

/*
================
SkillFixedDamage

pdmg and dmgt. Inferred: dmgt word 0 is the HP share and word 1 the MP
share of the damage actually dealt (the Bard's rows author 0 and 100,
and the description converts the damage into MP only). The HP share has
no owner, so a nonzero word 0 is refused rather than dropped. Without a
dmgt (Over Healing) MPPercent stays 0 and nothing is drained.
================
*/
type SkillFixedDamage struct {
	Present   bool
	Amount    uint32
	MPPercent uint32
	// Power is getv SAAA (+0x538): Formulae_CalculateFixedSkillDamage
	// (40F5F0) adds the caster's ParameterFixedDamagePower to the amount.
	Power bool
}

/*
================
compileSkillFixedDamage

Admit an instant or prepared handler-0 program with no att whose enemy
target is required, made of one pdmg, at most one dmgt, at most a
one-result cm, at most one kind-1 efr and known caster getv words. An area
never drains: only the single-target release pays dmgt back.
================
*/
func compileSkillFixedDamage(fields []string, row SkillRow) (SkillFixedDamage, SkillOffensiveArea, bool) {
	if len(fields) != 118 || fields[0] != "1" || fields[8] != "2" || fields[68] != "0" ||
		!row.TimingPinned || !row.Consumption.Pinned || !row.ActionRangePinned || row.ActionRange == 0 ||
		row.ChainSub || row.ChainNext != 0 || !row.ActionCastingTimePinned || row.ActionDurationMs == 0 ||
		row.Attack.Present || !row.TargetRequired || !row.Targets.EnemyM {
		return SkillFixedDamage{}, SkillOffensiveArea{}, false
	}
	// An enemy target only: no projectile, ground target, friendly or
	// secondary-target column.
	for _, column := range []int{15, 16, 17, 19, 20, 24, 25, 26, 27, 28, 31, 32, 33, 56} {
		if fields[column] != "0" {
			return SkillFixedDamage{}, SkillOffensiveArea{}, false
		}
	}
	program, err := CompileSkillProgram(fields)
	if err != nil {
		return SkillFixedDamage{}, SkillOffensiveArea{}, false
	}
	var out SkillFixedDamage
	var area SkillOffensiveArea
	seen := make(map[uint32]bool)
	for i := 0; i < program.Len(); i++ {
		op := program.Instruction(i)
		if seen[op.Tag] && op.Tag != tagGetv {
			return SkillFixedDamage{}, SkillOffensiveArea{}, false
		}
		seen[op.Tag] = true
		switch op.Tag {
		case tagFixedDamage:
			out.Amount = op.Arguments[0]
		case tagDamageTransfer:
			if op.Arguments[0] != 0 || op.Arguments[1] > maxDamageTransferPercent {
				return SkillFixedDamage{}, SkillOffensiveArea{}, false
			}
			out.MPPercent = op.Arguments[1]
		case uint32(skillMultiImpactTag):
			// Only the single result the release produces; more results
			// would need every hit and its drain counted.
			if op.Count != 2 || op.Arguments[0] != 2 || op.Arguments[1] != 1 {
				return SkillFixedDamage{}, SkillOffensiveArea{}, false
			}
		case tagEfr:
			a := op.Arguments
			if a[0] != 1 || a[1] < 1 || a[1] > 2 || a[2] == 0 || a[2] > 0xffff ||
				a[3] == 0 || a[3] > 255 || a[4] > 100 || a[5] > 0xff {
				return SkillFixedDamage{}, SkillOffensiveArea{}, false
			}
			area = SkillOffensiveArea{Shape: uint8(a[1]), Radius: a[2], MaxTargets: uint8(a[3]),
				ReductionPercent: uint8(a[4]), Select: uint8(a[5])}
		case tagGetv: // BDMD: the prepared cost applies it (noteParameterIndex)
			slot, known := SkillParameterFromKey(op.Arguments[0])
			if !known {
				return SkillFixedDamage{}, SkillOffensiveArea{}, false
			}
			out.Power = out.Power || slot == ParameterFixedDamagePower
		default:
			return SkillFixedDamage{}, SkillOffensiveArea{}, false
		}
	}
	if out.Amount == 0 || area.Radius != 0 && seen[tagDamageTransfer] {
		return SkillFixedDamage{}, SkillOffensiveArea{}, false
	}
	out.Present = true
	return out, area, true
}
