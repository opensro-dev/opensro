package enterworld

const skillCriticalTag = 0x6372 // "cr": the native cast stores the following pair at skill+244.

// SkillCriticalModifier belongs to the executing skill, not the character's
// permanent stats. Passive/buff cr blocks need their own ParamKeeper producer;
// parsing one here does not admit a passive as an attack.
type SkillCriticalModifier struct {
	Present       bool
	Flat, Percent uint32
}

// Read at parameter boundaries: an argument equal to "cr" is not a tag.
// The native parser overwrites +244 on a repeated cr block (last wins).
// Player offensive admission separately rejects duplicate effect blocks.
// Zero padding advances one word (client 84B322..84B32B); only ssou ends
// decoding. Do not replace this walk with a search for the numeric tag.
func encodedCriticalModifier(fields []string) (SkillCriticalModifier, bool) {
	var modifier SkillCriticalModifier
	for i := skilldataColEncodedTail; i < len(fields); {
		tag, ok := textdataInt(fields[i])
		if !ok || tag < 0 || tag > 0xffffffff {
			return modifier, false
		}
		if tag == 0x73736f75 {
			return modifier, true
		}
		if tag == skillCriticalTag {
			if i+2 >= len(fields) {
				return modifier, false
			}
			flat, flatOK := textdataInt(fields[i+1])
			percent, percentOK := textdataInt(fields[i+2])
			if !flatOK || !percentOK || flat < 0 || flat > 0xffffffff || percent < 0 || percent > 0xffffffff {
				return modifier, false
			}
			modifier = SkillCriticalModifier{Present: true, Flat: uint32(flat), Percent: uint32(percent)}
		}
		i += 1 + spawnParamArity(uint32(tag))
	}
	return modifier, true
}
