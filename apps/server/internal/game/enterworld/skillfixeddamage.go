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
no owner, so a nonzero word 0 is refused rather than dropped.
================
*/
type SkillFixedDamage struct {
	Present   bool
	Amount    uint32
	MPPercent uint32
}

/*
================
compileSkillFixedDamage

Admit an instant handler-0 program with no att whose enemy target is
required, made of one pdmg, one dmgt and known caster getv words.
================
*/
func compileSkillFixedDamage(fields []string, row SkillRow) (SkillFixedDamage, bool) {
	if len(fields) != 118 || fields[0] != "1" || fields[8] != "2" || fields[68] != "0" ||
		!row.TimingPinned || !row.Consumption.Pinned || !row.ActionRangePinned || row.ActionRange == 0 ||
		row.ChainSub || row.ChainNext != 0 || row.ActionCastingTimeMs != 0 || row.ActionDurationMs == 0 ||
		row.Attack.Present || !row.TargetRequired || !row.Targets.EnemyM {
		return SkillFixedDamage{}, false
	}
	// An enemy target only: no projectile, ground target, friendly or
	// secondary-target column.
	for _, column := range []int{15, 16, 17, 19, 20, 24, 25, 26, 27, 28, 31, 32, 33, 56} {
		if fields[column] != "0" {
			return SkillFixedDamage{}, false
		}
	}
	program, err := CompileSkillProgram(fields)
	if err != nil {
		return SkillFixedDamage{}, false
	}
	var out SkillFixedDamage
	seen := make(map[uint32]bool)
	for i := 0; i < program.Len(); i++ {
		op := program.Instruction(i)
		if seen[op.Tag] && op.Tag != tagGetv {
			return SkillFixedDamage{}, false
		}
		seen[op.Tag] = true
		switch op.Tag {
		case tagFixedDamage:
			out.Amount = op.Arguments[0]
		case tagDamageTransfer:
			if op.Arguments[0] != 0 || op.Arguments[1] > maxDamageTransferPercent {
				return SkillFixedDamage{}, false
			}
			out.MPPercent = op.Arguments[1]
		case tagGetv: // BDMD: the prepared cost applies it (noteParameterIndex)
			if _, known := SkillParameterFromKey(op.Arguments[0]); !known {
				return SkillFixedDamage{}, false
			}
		default:
			return SkillFixedDamage{}, false
		}
	}
	if out.Amount == 0 || !seen[tagDamageTransfer] {
		return SkillFixedDamage{}, false
	}
	out.Present = true
	return out, true
}
