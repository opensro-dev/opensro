/*
===========================================================================

skillmonsterself.go - monster self-buff skill rows

Two programs a monster casts on itself: the timed self buff its AI casts
from a conditional entry (defp, cr, dru), and the persistent mom buff an
event casts (the Roc's hour-mark enrage). Each row type says what the
runtime may install; the stat writes live in combat/monster_self_effect.go.

===========================================================================
*/

package enterworld

/*
================
SkillMonsterSelfEffect

A complete, unlinked monster self-buff program. The self selector and
ContinueBasicAttack column distinguish it from player timed effects.
Admission still measures the enemy's body-contact radius.
================
*/
type SkillMonsterSelfEffect struct {
	Pinned        bool
	Tag           uint32
	First, Second uint32
}

/*
================
parseSkillMonsterSelfEffect
================
*/
func parseSkillMonsterSelfEffect(fields []string, row *SkillRow) {
	if len(fields) != 118 || fields[0] != "1" || fields[8] != "2" || fields[68] != "3" ||
		fields[9] != "0" || fields[19] != "1" || fields[26] != "1" ||
		!row.ActionCastingTimePinned || !row.ActionDurationPinned || !row.TimingPinned ||
		!row.ActionRangePinned || !row.ReplacementPinned || row.CoolTimeMs == 0 ||
		!row.SpawnToken || row.SpawnStatus || row.EffectRider || !row.EffectDurationPresent || row.EffectDurationMs == 0 {
		return
	}
	// No implicit target domains, resource debits, projectiles or repeats.
	for _, col := range []int{16, 17, 20, 21, 22, 23, 24, 25, 27, 28, 29, 30, 31, 32, 33, 52, 53, 54, 55, 56} {
		if fields[col] != "0" {
			return
		}
	}
	if fields[50] != "255" || fields[51] != "255" {
		return
	}
	program, err := CompileSkillProgram(fields)
	if err != nil || program.Len() != 2 {
		return
	}
	duration, effect := program.Instruction(0), program.Instruction(1)
	if duration.Tag != 0x64757261 || duration.Count != 1 || duration.Arguments[0] != row.EffectDurationMs {
		return
	}
	switch effect.Tag {
	case 0x64656670:
		if effect.Count != 3 || effect.Arguments[2] != 0 {
			return
		}
	case 0x6372, 0x647275:
		if effect.Count != 2 {
			return
		}
	default:
		return
	}
	row.MonsterSelfEffect = SkillMonsterSelfEffect{Pinned: true, Tag: effect.Tag, First: effect.Arguments[0], Second: effect.Arguments[1]}
}

// TagMonsterStacking is the mom program: a monster's persistent self buff
// that grows in steps (monster.SelfEffect.Steps, combat's stat writes).
const TagMonsterStacking = 0x6d6f6d // mom

/*
================
SkillMonsterStackingEffect

A monster's persistent self buff that grows in steps (mom: period, step,
an unread word, multiplier). The v1.188 server's persistent pass counts a
step when the cast lands and one more each PeriodMs after; each pass
writes count * multiplier * step (StepValue) to the caster's damage
rates, defences, evasion and hit rate (combat/monster_self_effect.go).
It has no duration: it lasts while the caster lives. No AI path casts
it; an event does (the Roc's hour mark, action/roc.go).
================
*/
type SkillMonsterStackingEffect struct {
	Pinned    bool
	PeriodMs  uint32
	StepValue uint32
}

/*
================
parseSkillMonsterStackingEffect

A self-targeted row whose whole program is one mom instruction, with no
duration of its own (the event casts it, so its reuse delay is unread).
================
*/
func parseSkillMonsterStackingEffect(fields []string, row *SkillRow) {
	if len(fields) != 118 || fields[0] != "1" || fields[26] != "1" || row.EffectDurationPresent {
		return
	}
	program, err := CompileSkillProgram(fields)
	if err != nil || program.Len() != 1 {
		return
	}
	mom := program.Instruction(0)
	if mom.Tag != TagMonsterStacking || mom.Count != 4 {
		return
	}
	period, step, multiplier := mom.Arguments[0], mom.Arguments[1], mom.Arguments[3]
	if period == 0 || step == 0 || multiplier == 0 {
		return
	}
	row.MonsterStackingEffect = SkillMonsterStackingEffect{Pinned: true, PeriodMs: period, StepValue: step * multiplier}
}
