/*
===========================================================================

skillthreatdecrease.go - damage-free programs that lower monster hostility

The Bard's Discord Wave (SKILL_EU_BARD_FORGETA_AGGRO_A) is
efr(1,2,100,4,0,16) ovl2(34) dtnt(flat,0) mwdt(850) getv(BDMD), cast on a
friendly target (columns 22 and 26..28: required, self, ally, party). Its
description: "Removes Monsters' hostility toward their target by creating
a big wave of discord around the target." The action owner is
action/discordwave.go; admission is by executable shape, never by name.

The Warlock's Mirage and Phantasma author the caster-centred, untargeted
form (efr shape 1, no Self/Ally/Party) and are not admitted here.

===========================================================================
*/

package enterworld

const (
	// tagThreatDecrease is dtnt {flat, percent}: the client tooltip
	// (sub_7f9bd0, +0x19C) prints "Aggro <flat> decrease" and
	// "Aggro <percent>% decrease".
	tagThreatDecrease = 0x64746e74

	// tagMagicalWeaponDecrease is mwdt {percent}, the "Weapon Magical
	// Attack Power <percent>% Reflect" row (+0x28C) of the weapon-term
	// family pwtt/mwtt (taunt), pwdt/mwdt (decrease), mwhh/mwmh (heal).
	tagMagicalWeaponDecrease = 0x6d776474

	// decreaseAreaShape is efr shape 2: centred on the selected target.
	decreaseAreaShape = 2

	// decreaseAreaSelect is efr select 16 (0x10), the non-character
	// objects of TargetSelection_AroundSource (58A020): monsters. The
	// Warrior's taunts select their victims with the same word.
	decreaseAreaSelect = 16
)

/*
==================
compileSkillThreatDecrease

Admit an instant handler-0 program with no att whose friendly target is
required, made of one primary-centred efr selecting monsters, one dtnt,
an optional mwdt weapon term, ovl2 and known caster getv words.
==================
*/
func compileSkillThreatDecrease(fields []string, row SkillRow) (SkillThreat, bool) {
	if len(fields) != 118 || fields[0] != "1" || fields[8] != "2" || fields[68] != "0" ||
		!row.TimingPinned || !row.Consumption.Pinned || !row.ActionRangePinned || row.ActionRange == 0 ||
		row.ChainSub || row.ChainNext != 0 || row.ActionCastingTimeMs != 0 || row.ActionDurationMs == 0 ||
		row.Attack.Present || !row.TargetRequired {
		return SkillThreat{}, false
	}
	// A friendly target only: Self, Ally and Party, never an enemy.
	if fields[26] != "1" || fields[27] != "1" || fields[28] != "1" || fields[29] != "0" || fields[30] != "0" {
		return SkillThreat{}, false
	}
	for _, column := range []int{15, 16, 17, 19, 20, 24, 25, 31, 32, 33, 56} {
		if fields[column] != "0" {
			return SkillThreat{}, false
		}
	}
	program, err := CompileSkillProgram(fields)
	if err != nil {
		return SkillThreat{}, false
	}
	var out SkillThreat
	seen := make(map[uint32]bool)
	for i := 0; i < program.Len(); i++ {
		op := program.Instruction(i)
		if seen[op.Tag] && op.Tag != tagGetv {
			return SkillThreat{}, false
		}
		seen[op.Tag] = true
		switch op.Tag {
		case tagEfr:
			a := op.Arguments
			if a[0] != 1 || a[1] != decreaseAreaShape || a[2] == 0 || a[2] > 0xffff ||
				a[3] == 0 || a[3] > 255 || a[4] != 0 || a[5] != decreaseAreaSelect {
				return SkillThreat{}, false
			}
			out.Area = SkillOffensiveArea{Shape: decreaseAreaShape, Radius: a[2], MaxTargets: uint8(a[3]), Select: decreaseAreaSelect}
		case tagThreatDecrease:
			out.DecreaseFlat, out.DecreasePercent = op.Arguments[0], op.Arguments[1]
		case tagMagicalWeaponDecrease:
			out.DecreaseWeaponPercent = op.Arguments[0]
		case tagTimedOverlap: // ovl2: the replacement descriptor's casting-state word
		case tagGetv: // BDMD: the prepared cost applies it (noteParameterIndex)
			if _, known := SkillParameterFromKey(op.Arguments[0]); !known {
				return SkillThreat{}, false
			}
		default:
			return SkillThreat{}, false
		}
	}
	if !seen[tagThreatDecrease] || out.Area.Radius == 0 || out.DecreasePercent > 100 {
		return SkillThreat{}, false
	}
	out.Decrease = true
	return out, true
}
