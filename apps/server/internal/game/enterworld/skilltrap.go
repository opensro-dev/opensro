/*
===========================================================================

skilltrap.go - complete quest-trap programs from the native skill table

The indirect item path creates a stationary CGSkillObject, not a resident
character buff. Keep its authored targets and lifetime together so admission,
world scans and quest dispatch cannot disagree about the same skill.

===========================================================================
*/
package enterworld

const (
	questTrapTag     = 0x74726170
	questTrapQuest   = 0x71657374
	questTrapArea    = 0x656672
	questTrapLink    = 0x6c6e6b73
	questTrapDura    = 0x64757261
	questTrapScanMs  = 300
	questTrapMobMask = 16
)

/*
================
SkillQuestTrap

48D690 stops matching at the first zero target. Preserve all three words,
including empty lists used by the promotion item rows in v1.150 media.
================
*/
type SkillQuestTrap struct {
	Present    bool
	DurationMs uint32
	ScanMs     uint32
	Radius     uint32
	Targets    [3]uint32
}

/*
================
compileQuestTrap

48CCC0 and 48CEA0 own qest mode 1, stationary monster scans and expiry.
Require the complete shipped program. Unknown instructions cannot be ignored
just because a familiar trap tag appears elsewhere in the row.
================
*/
func compileQuestTrap(fields []string) SkillQuestTrap {
	program, err := CompileSkillProgram(fields)
	if err != nil || program.Len() != 5 {
		return SkillQuestTrap{}
	}
	var trap SkillQuestTrap
	seen := make(map[uint32]bool)
	for index := 0; index < program.Len(); index++ {
		instruction := program.Instruction(index)
		if seen[instruction.Tag] {
			return SkillQuestTrap{}
		}
		seen[instruction.Tag] = true
		words := instruction.Arguments
		switch instruction.Tag {
		case questTrapQuest:
			if words[0] != 1 {
				return SkillQuestTrap{}
			}
			trap.Targets = [3]uint32{words[1], words[2], words[3]}
		case questTrapDura:
			if words[0] == 0 {
				return SkillQuestTrap{}
			}
			trap.DurationMs = words[0]
		case questTrapArea:
			if words[0] != 3 || words[1] != 1 || words[2] == 0 || words[3] != 1 ||
				words[4] != 0 || words[5] != questTrapMobMask {
				return SkillQuestTrap{}
			}
			trap.Radius = words[2]
		case questTrapLink:
			if words[0] != 0 || words[1] != questTrapScanMs || words[2] != 1 || words[3] != 0 {
				return SkillQuestTrap{}
			}
			trap.ScanMs = words[1]
		case questTrapTag:
		default:
			return SkillQuestTrap{}
		}
	}
	trap.Present = true
	return trap
}
