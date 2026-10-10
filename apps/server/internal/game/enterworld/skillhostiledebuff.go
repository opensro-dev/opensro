/*
===========================================================================

skillhostiledebuff.go - a timed buff instance cast on an enemy

Vital Spot-Muscle and Vital Spot-Spirit (SKILL_CH_WATER_CANCEL_A / _B)
are handler-3 buffs whose required target is an enemy: bbuf, dura, one
terd or thrd word and tant. The target's skill manager installs the
instance like any buff, and CSkillManager_ApplyBuffModifiersToActor
(594AC0) writes the word, negated, to the recipient's parameter on the
flat channel: terd (+0x2DC) to 9, evasion (59591A), thrd (+0x2E0) to 0xB,
hit rate (595954). tagRefSkill_MatchesExecutionSelector reads both words
(589E9B, 589EBF), so the cast is a hostile execution: attack rules admit a
player target. tant is the aggression the hit leaves on a monster.

===========================================================================
*/

package enterworld

const (
	// tagEvasionDecrease is terd (one word); tagHitRateDecrease is thrd.
	tagEvasionDecrease = 0x74657264
	tagHitRateDecrease = 0x74687264
)

/*
================
SkillHostileDebuff

The compiled enemy-targeted buff: its lifetime, the evasion or hit-rate
decrease it writes and the authored aggression (tant flat, percent).
================
*/
type SkillHostileDebuff struct {
	Pinned        bool
	DurationMs    uint32
	Evasion       uint32
	HitRate       uint32
	ThreatFlat    uint32
	ThreatPercent uint32
}

/*
================
compileSkillHostileDebuff

Admit the Vital Spot shape: handler 3, a required target with a range
that names an enemy (Enemy_M or Enemy_P) and no friend, no preparation
time, no projectile or repeat columns, and a program of bbuf, dura (the row's effect duration),
exactly one of terd or thrd, tant and optional reqi pairs. Anything else
refuses.
================
*/
func compileSkillHostileDebuff(fields []string, row SkillRow) SkillHostileDebuff {
	if len(fields) != 118 || fields[0] != "1" || fields[8] != "2" || fields[68] != "3" ||
		!row.TargetRequired || row.ActionRange == 0 || row.ChainSub || row.ChainNext != 0 || row.ActionCastingTimeMs != 0 ||
		!row.TimingPinned || !row.Consumption.Pinned || !row.ActionRangePinned || row.EffectDurationMs == 0 {
		return SkillHostileDebuff{}
	}
	if fields[22] != "1" || fields[23] != "1" || fields[29] != "1" && fields[30] != "1" {
		return SkillHostileDebuff{}
	}
	for _, column := range []int{15, 16, 17, 20, 24, 25, 26, 27, 28, 31, 32, 33, 56} {
		if fields[column] != "0" {
			return SkillHostileDebuff{}
		}
	}
	program, err := CompileSkillProgram(fields)
	if err != nil {
		return SkillHostileDebuff{}
	}
	var out SkillHostileDebuff
	seen := make(map[uint32]bool)
	for i := 0; i < program.Len(); i++ {
		op := program.Instruction(i)
		if seen[op.Tag] && op.Tag != tagReqi {
			return SkillHostileDebuff{}
		}
		seen[op.Tag] = true
		switch op.Tag {
		case tagBbuf, tagReqi:
		case tagDura:
			if op.Count != 1 || op.Arguments[0] != row.EffectDurationMs {
				return SkillHostileDebuff{}
			}
			out.DurationMs = op.Arguments[0]
		case tagEvasionDecrease:
			out.Evasion = op.Arguments[0]
		case tagHitRateDecrease:
			out.HitRate = op.Arguments[0]
		case tagStatusThreat:
			out.ThreatFlat, out.ThreatPercent = op.Arguments[0], op.Arguments[1]
		default:
			return SkillHostileDebuff{}
		}
	}
	if out.DurationMs == 0 || !seen[tagStatusThreat] || (out.Evasion == 0) == (out.HitRate == 0) {
		return SkillHostileDebuff{}
	}
	out.Pinned = true
	return out
}

/*
================
SkillHostileDebuff.Word

The one program word the instance carries: its tag (terd or thrd) and the
decrease. A monster stores exactly this pair in its effect slot.
================
*/
func (d SkillHostileDebuff) Word() (tag, value uint32) {
	if d.Evasion != 0 {
		return tagEvasionDecrease, d.Evasion
	}
	return tagHitRateDecrease, d.HitRate
}
