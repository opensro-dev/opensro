/*
===========================================================================

skillitemeffect.go - compile complete item-owned stat effect programs

Native 49C2B0 cases 1..3 resolve the item's skill; 59B8D0 turns cbuf+dura
into an owner timed job. Admission follows the complete descriptor program,
never an item name. Unknown or compound unsupported instructions keep the
entire item refused rather than consuming it for a partial effect.

===========================================================================
*/
package enterworld

const (
	itemEffectDuration = 0x64757261
	itemEffectOwnerJob = 0x63627566
	itemEffectHP       = 0x687069
	itemEffectMP       = 0x6d7069
	itemEffectEvasion  = 0x6572
	itemEffectHit      = 0x6872
	itemEffectDamage   = 0x647275
	itemEffectAbsorb   = 0x6f646172
	itemEffectSTR      = 0x73747269
	itemEffectINT      = 0x696e7469
	itemEffectRecovery = 0x69726763
	itemEffectGold     = 0x676472
)

/*
================
SkillFlatRate

The native block supplies independent flat and percent-add contributions,
unlike stri/inti whose second word caps an additive contribution.
================
*/
type SkillFlatRate struct {
	Present       bool
	Flat, Percent uint32
}

/*
================
compileTimedItemEffect

Only direct, self-owned timed jobs are admitted. Costs, target selection,
links, summons and repeating programs need their own complete producers.
================
*/
func compileTimedItemEffect(fields []string, row SkillRow) (SkillTimedEffect, bool) {
	var result SkillTimedEffect
	if len(fields) != 118 || fields[0] != "1" || fields[8] != "1" || fields[68] != "3" ||
		row.ChainNext != 0 || !row.ReplacementPinned || row.EffectDurationMs == 0 {
		return result, false
	}
	for _, col := range []int{9, 12, 13, 15, 16, 17, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 53, 54, 55, 56} {
		if fields[col] != "0" {
			return result, false
		}
	}
	program, err := CompileSkillProgram(fields)
	if err != nil {
		return result, false
	}
	seen := make(map[uint32]bool)
	modifiers := false
	for i := 0; i < program.Len(); i++ {
		op := program.Instruction(i)
		if seen[op.Tag] {
			return SkillTimedEffect{}, false
		}
		seen[op.Tag] = true
		pair := SkillFlatRate{Present: true, Flat: op.Arguments[0], Percent: op.Arguments[1]}
		switch op.Tag {
		case itemEffectOwnerJob:
			result.Persistent = true
		case itemEffectDuration:
			if op.Arguments[0] == 0 || op.Arguments[0] != row.EffectDurationMs {
				return SkillTimedEffect{}, false
			}
		case 0x6e627566, 0x62627566: // nbuf/bbuf: existing cancellation/board owners
		case itemEffectHP:
			result.HP = pair
			modifiers = true
		case itemEffectMP:
			result.MP = pair
			modifiers = true
		case itemEffectEvasion:
			result.Evasion = pair
			modifiers = true
		case itemEffectHit:
			result.Accuracy = pair
			modifiers = true
		case itemEffectSTR:
			result.Strength = SkillStatBoost{Present: true, Value: pair.Flat, CapPercent: pair.Percent}
			modifiers = true
		case itemEffectINT:
			result.Intellect = SkillStatBoost{Present: true, Value: pair.Flat, CapPercent: pair.Percent}
			modifiers = true
		case itemEffectRecovery:
			// 595A33..595A93: independent percent-sum writes to HP/MP
			// recovery parameters, not flat healing or maximum gauges.
			result.Recovery = SkillRecoveryRates{Present: true, HP: pair.Flat, MP: pair.Percent}
			modifiers = true
		case itemEffectGold:
			result.GoldDropPercent = op.Arguments[0]
			modifiers = true
		case itemEffectDamage, itemEffectAbsorb:
			// parseSkillOffense already decodes these shared modifier blocks;
			// the common installer owns their arithmetic and removal.
			modifiers = true
		default:
			return SkillTimedEffect{}, false
		}
	}
	result.Pinned = result.Persistent && seen[itemEffectDuration] && modifiers
	result.ItemProgram = result.Pinned
	return result, result.Pinned
}
