package enterworld

// SkillPassiveDefense is a complete flat defp program. The server's native
// code installs Param5/6 channel 0. Any reqi/reqn it carries (row.Reqi) decides
// whether the passive contributes: its equipment check switches its modifiers off while
// the equipment fails the walk. Caps, duration and linked stages require
// separate contracts; no operation is silently skipped.
type SkillPassiveDefense struct {
	Pinned            bool
	Physical, Magical uint32
}

func encodedPassiveDefense(fields []string) SkillPassiveDefense {
	if len(fields) != 118 || fields[0] != "1" || fields[8] != "0" || fields[9] != "0" || fields[68] != "4" {
		return SkillPassiveDefense{}
	}
	program, err := CompileSkillProgram(fields)
	if err != nil {
		return SkillPassiveDefense{}
	}
	var out SkillPassiveDefense
	seen := false
	for i := 0; i < program.Len(); i++ {
		op := program.Instruction(i)
		switch op.Tag {
		case 0x64656670:
			if seen || op.Count != 3 || op.Arguments[2] != 0 {
				return SkillPassiveDefense{}
			}
			seen = true
			out.Physical = op.Arguments[0]
			out.Magical = op.Arguments[1]
		case 0x72657169, 0x7265716e: // reqi/reqn: row.Reqi, evaluated by combat.ReqiRefusal
		default:
			return SkillPassiveDefense{}
		}
	}
	out.Pinned = seen
	return out
}
