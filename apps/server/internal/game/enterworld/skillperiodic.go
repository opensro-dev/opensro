/*
===========================================================================

skillperiodic.go - complete linked damage-over-time programs

The persistent handler owns these attacks independently of the casting actor.
Compiling a pulse does not admit it as an instant offensive sequence.

===========================================================================
*/

package enterworld

import "opensro.online/server/internal/game/abnormal"

const (
	tagPeriodicPerTarget = 0x6c6b7332
	parameterDotPower    = 0x44544154
	parameterDotDuration = 0x44544452
)

/*
================
SkillPeriodicEffect

One source/recipient linked attack. Native 5830B0 uses puls for the source
half's attack clock and dura for the recipient lifetime. lks2 changes outgoing
admission from a caster-wide count to a count against each recipient.
================
*/
type SkillPeriodicEffect struct {
	Pinned               bool
	Attack               SkillAttack
	Area                 SkillOffensiveArea
	Link                 SkillEffectLink
	DurationMs, PeriodMs uint32
}

/*
================
compileSkillPeriodicEffect

Only the complete hostile linked program reaches this producer. Keeping its
attack inside the descriptor prevents the ordinary cast path from dealing an
extra hit or charging resources on each pulse.
================
*/
func compileSkillPeriodicEffect(fields []string, row SkillRow) SkillPeriodicEffect {
	var out SkillPeriodicEffect
	if len(fields) != 118 || fields[0] != "1" || fields[8] != "2" || fields[68] != "3" ||
		row.ChainNext != 0 || !row.Consumption.Pinned || !row.ActionCastingTimePinned ||
		!row.ActionDurationPinned || !row.TimingPinned || !row.ReplacementPinned ||
		!row.ActionRangePinned || !row.TargetRequired || row.Consumption.HP != 0 || row.Consumption.HPPercent != 0 {
		return out
	}
	for _, column := range []int{15, 16, 17, 19, 20, 24, 25, 26, 27, 28, 31, 32, 33, 56} {
		if fields[column] != "0" {
			return out
		}
	}
	for _, column := range []int{22, 23, 29, 30} {
		if fields[column] != "1" {
			return out
		}
	}
	program, err := CompileSkillProgram(fields)
	if err != nil {
		return out
	}
	seen := make(map[uint32]bool)
	statusCount := 0
	for i := 0; i < program.Len(); i++ {
		op := program.Instruction(i)
		if seen[op.Tag] && op.Tag != tagGetv {
			return SkillPeriodicEffect{}
		}
		seen[op.Tag] = true
		switch op.Tag {
		case tagNbuf, tagBbuf, tagPeriodicPerTarget:
		case tagTimedLink:
			if op.Arguments[0] == 0 || op.Arguments[1] != 0 || op.Arguments[2] == 0 || op.Arguments[3] != 0 {
				return SkillPeriodicEffect{}
			}
			out.Link = SkillEffectLink{Present: true, Group: op.Arguments[0], MaxOutgoing: op.Arguments[2]}
		case tagDura:
			out.DurationMs = op.Arguments[0]
		case skillPulseTag:
			out.PeriodMs = op.Arguments[0]
		case uint32(skillAttackTag):
			if op.Arguments[0] != 8 || op.Arguments[1] == 0 || op.Arguments[2] > op.Arguments[3] || op.Arguments[4] != 0 {
				return SkillPeriodicEffect{}
			}
			out.Attack = SkillAttack{Present: true, Flags: op.Arguments[0], Percent: int64(op.Arguments[1]),
				Min: int64(op.Arguments[2]), Max: int64(op.Arguments[3]), ImpactCount: 1}
		case uint32(skillMultiImpactTag):
			if op.Arguments[0] != 2 || op.Arguments[1] != 1 {
				return SkillPeriodicEffect{}
			}
		case tagEfr:
			if op.Arguments[0] != 1 || op.Arguments[1] != 2 || op.Arguments[2] == 0 ||
				op.Arguments[3] == 0 || op.Arguments[3] > 255 || op.Arguments[4] != 0 || op.Arguments[5] != 24 {
				return SkillPeriodicEffect{}
			}
			out.Area = SkillOffensiveArea{Shape: 2, Radius: op.Arguments[2], MaxTargets: uint8(op.Arguments[3]), Select: 24}
		case 0x6275, 0x7073, 0x626c, 0x736c:
			source, known := abnormal.SourceIndex(op.Tag)
			if !known || !validAbnormalBlock(fields, int(op.Column), abnormal.Sources[source]) {
				return SkillPeriodicEffect{}
			}
			statusCount++
		case tagGetv:
			if op.Arguments[0] != parameterDotPower && op.Arguments[0] != parameterDotDuration {
				return SkillPeriodicEffect{}
			}
		default:
			return SkillPeriodicEffect{}
		}
	}
	if !out.Attack.Present || !out.Link.Present || !seen[tagPeriodicPerTarget] ||
		!seen[tagNbuf] || !seen[tagBbuf] || out.DurationMs == 0 || out.PeriodMs == 0 || statusCount != 1 {
		return SkillPeriodicEffect{}
	}
	out.Attack.Parameters = encodedAttackParameters(fields)
	out.Pinned = true
	return out
}
