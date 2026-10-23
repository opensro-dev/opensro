/*
===========================================================================

skillduplicate.go - the Rogue's Duplicate and Sable Duplicate

msch 2 {level} dura skc [reqi]: the Rogue takes the look of an allied
player no higher than the level word (the native target validation). The
instance lives on the caster; the native persistent-cast lifecycle stores
the copied player's gid in its context and the native duplicate transform
copies that player's model and worn equipment. skc ends it on the next
skill cast.

===========================================================================
*/

package enterworld

// SkillDuplicate is the executable msch 2 program.
type SkillDuplicate struct {
	Pinned   bool
	MaxLevel uint32
}

func parseSkillDuplicate(fields []string, row *SkillRow) {
	gate := row.CastGate
	if !gate.MschPresent || gate.MschMode != 2 || gate.MschLevel == 0 {
		return
	}
	if !row.ReplacementPinned || !row.TimingPinned || !row.ActionCastingTimePinned || row.ActionCastingTimeMs != 0 ||
		!row.Consumption.Pinned || row.ChainNext != 0 || row.ActionHandler != SkillActionPersistent {
		return
	}
	if !row.TargetRequired || row.Targets.EnemyM || row.Targets.EnemyP || !row.Targets.Ally && !row.Targets.Party {
		return
	}
	program, err := CompileSkillProgram(fields)
	if err != nil {
		return
	}
	duration := false
	for i := 0; i < program.Len(); i++ {
		switch op := program.Instruction(i); op.Tag {
		case 0x6d736368: // msch
		case tagDura:
			duration = op.Arguments[0] != 0
		case tagSkc, tagReqi:
		default:
			return
		}
	}
	if !duration {
		return
	}
	row.Duplicate = SkillDuplicate{Pinned: true, MaxLevel: gate.MschLevel}
}
