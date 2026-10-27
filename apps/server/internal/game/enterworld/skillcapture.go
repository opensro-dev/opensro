/*
===========================================================================

skillcapture.go - the Rogue's Monster Mask and Beast Mask

mcap {max level, item RefObj} (RefSkill +0x4A8) turns a dead monster into
an Essence of the Dead: the server's target validation
admits a normal-grade, TID4 1 monster no higher than the cap, and
its effect application drops the item at the
caster's feet, owned by the caster, holding the corpse's RefObj.

===========================================================================
*/

package enterworld

const tagMcap = 0x6d636170

// SkillMonsterCapture is the executable mcap program. Pinned rows are the
// instant, corpse-targeted ones whose program is mcap alone.
type SkillMonsterCapture struct {
	Pinned       bool
	MaxLevel     uint32
	ItemRefObjID uint32
}

func parseSkillMonsterCapture(fields []string, row *SkillRow) {
	if !row.ReplacementPinned || !row.TimingPinned || !row.ActionCastingTimePinned ||
		!row.Consumption.Pinned || row.ChainNext != 0 || row.ActionHandler != SkillActionInstant {
		return
	}
	if !row.TargetRequired || !row.Targets.EnemyM || !row.Targets.DeadBody {
		return
	}
	program, err := CompileSkillProgram(fields)
	if err != nil || program.Len() != 1 {
		return
	}
	op := program.Instruction(0)
	if op.Tag != tagMcap || op.Arguments[0] == 0 || op.Arguments[1] == 0 {
		return
	}
	row.MonsterCapture = SkillMonsterCapture{Pinned: true, MaxLevel: op.Arguments[0], ItemRefObjID: op.Arguments[1]}
}
