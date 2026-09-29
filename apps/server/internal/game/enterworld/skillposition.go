/*
===========================================================================

skillposition.go - complete ground travel and target-charge descriptors

Position effects retain their native range independently of cast duration.
Offensive admission owns the rest of a charge's complete instruction stream.

===========================================================================
*/

package enterworld

const tagPositionCharge = 0x74656c33

/*
================
SkillPositionEffect

5862E0 reads argument one as range and selects travel bit eight. Pinned owns
ground targeting; Charge keeps an offensive target and guided arrival.
================
*/
type SkillPositionEffect struct {
	Pinned           bool
	Charge           bool
	Parameter, Range uint32
}

/*
================
decodeSkillPosition

A charge is enabled only after the whole offensive program was admitted.
The standalone tele branch continues to require its exact ground envelope.
================
*/
func decodeSkillPosition(fields []string, row SkillRow) SkillPositionEffect {
	p, err := CompileSkillProgram(fields)
	if err == nil && row.OffensiveStagePinned && row.CastGate.Tel3 {
		for index := 0; index < p.Len(); index++ {
			op := p.Instruction(index)
			if op.Tag == tagPositionCharge {
				return SkillPositionEffect{Charge: true, Parameter: op.Arguments[0], Range: op.Arguments[1]}
			}
		}
	}
	if err != nil || p.Len() != 1 || !row.TimingPinned || !row.Consumption.Pinned ||
		!row.TargetRequired || row.ChainSub || row.ChainNext != 0 ||
		row.ActionCastingTimeMs != 0 || row.ActionDurationMs != 0 ||
		row.Consumption.HP != 0 || row.Consumption.HPPercent != 0 ||
		fields[0] != "1" || fields[15] != "0" || fields[17] != "0" || fields[56] != "0" {
		return SkillPositionEffect{}
	}
	i := p.Instruction(0)
	if i.Tag != 0x74656c65 || i.Count != 2 || i.Arguments[1] == 0 || i.Arguments[1] > 0x7fffffff {
		return SkillPositionEffect{}
	}
	return SkillPositionEffect{Pinned: true, Parameter: i.Arguments[0], Range: i.Arguments[1]}
}
