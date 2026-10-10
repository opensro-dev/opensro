/*
===========================================================================

skillposition.go - complete ground travel and target-charge descriptors

Position effects retain their native range independently of cast duration.
Offensive admission owns the rest of a charge's complete instruction stream.

===========================================================================
*/

package enterworld

const (
	tagPositionTravel   = 0x74656c65 // tele
	tagPositionTeleport = 0x74656c32 // tel2
	tagPositionCharge   = 0x74656c33 // tel3
)

/*
================
SkillPositionEffect

5862E0 reads argument one as range and selects travel bit eight. Pinned owns
ground targeting; Charge keeps an offensive target and guided arrival.
================
*/
type SkillPositionEffect struct {
	Pinned bool
	Charge bool
	// Parameter is argument zero, 500 in every shipped tele, tel2 and tel3
	// row. The original reads it nowhere (#519): SkillGlobal_BuildParameterIndex
	// binds the blocks at +2EC / +2F0 / +2F4, SkillAction_ApplyPositionEffect
	// (5862E0) loads only word +4 of each, and SkillAction_Instant (586B41,
	// 586F5C), SkillAction_Projectile (585A87) and
	// Skill_ValidatePrerequisitesAndCost (58E019) only test the blocks'
	// presence. Kept for the parse; no rule depends on it.
	Parameter uint32
	Range     uint32
}

/*
================
decodeSkillPosition

A charge is enabled only after the whole offensive program was admitted.
The standalone tele/tel2 branch continues to require its exact ground envelope.
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
	if err != nil || !row.TimingPinned || !row.Consumption.Pinned ||
		!row.TargetRequired || row.ChainSub || row.ChainNext != 0 ||
		row.ActionCastingTimeMs != 0 || row.ActionDurationMs != 0 ||
		row.Consumption.HP != 0 || row.Consumption.HPPercent != 0 ||
		fields[0] != "1" || fields[15] != "0" || fields[17] != "0" || fields[56] != "0" {
		return SkillPositionEffect{}
	}
	// One ground-travel instruction: tele (Ghost Walk) or tel2 (the Wizard's
	// Teleport, which 58E010 leaves usable while rooted). Caster getv
	// modifiers (WIMD) only adjust the prepared cost.
	var travel SkillInstruction
	for index := 0; index < p.Len(); index++ {
		op := p.Instruction(index)
		switch op.Tag {
		case tagPositionTravel, tagPositionTeleport:
			if travel.Tag != 0 {
				return SkillPositionEffect{}
			}
			travel = op
		case tagGetv:
			if _, known := SkillParameterFromKey(op.Arguments[0]); !known {
				return SkillPositionEffect{}
			}
		default:
			return SkillPositionEffect{}
		}
	}
	if travel.Tag == 0 || travel.Count != 2 || travel.Arguments[1] == 0 || travel.Arguments[1] > 0x7fffffff {
		return SkillPositionEffect{}
	}
	return SkillPositionEffect{Pinned: true, Parameter: travel.Arguments[0], Range: travel.Arguments[1]}
}
