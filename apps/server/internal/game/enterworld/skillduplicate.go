/*
===========================================================================

skillduplicate.go - the Rogue's Duplicate and Sable Duplicate

msch 2 {level} dura skc [reqi]: the Rogue takes the look of another
player no higher than the level word (TargetValidation 58D23B). The
native ranks target allied players; the later rank targets an enemy
player. The reference server admits both envelopes for this family:
its Duplicate branch gates only the target's job suit, battle mode,
level, GM privilege and murderer state, never the pair's relation.
The instance lives on the caster; CastLifecycle_ProcessPersistent (583BC6)
stores the copied player's gid in its context and
CGObjPC_ApplyDupleTransform (4F0040) copies that player's model and worn
equipment (4F0320). skc ends it on the next skill cast.

===========================================================================
*/

package enterworld

// SkillDuplicate is the executable msch 2 program.
type SkillDuplicate struct {
	Pinned   bool
	MaxLevel uint32
}

/*
================
parseSkillDuplicate

Pins the executable msch 2 program. The target envelope admits the
allied ranks (Ally or Party) and the enemy-player rank (EnemyP): the
reference server copies whichever player the row's own target classes
admit, so only a monster-targeted or untargeted row falls outside
this family.
================
*/
func parseSkillDuplicate(fields []string, row *SkillRow) {
	gate := row.CastGate
	if !gate.MschPresent || gate.MschMode != 2 || gate.MschLevel == 0 {
		return
	}
	if !row.ReplacementPinned || !row.TimingPinned || !row.ActionCastingTimePinned || row.ActionCastingTimeMs != 0 ||
		!row.Consumption.Pinned || row.ChainNext != 0 || row.ActionHandler != SkillActionPersistent {
		return
	}
	if !row.TargetRequired || row.Targets.EnemyM || !row.Targets.Ally && !row.Targets.Party && !row.Targets.EnemyP {
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
