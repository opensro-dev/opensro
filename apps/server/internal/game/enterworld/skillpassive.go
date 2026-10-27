package enterworld

// SkillPassiveCritical is the completely admitted flat cr + one weapon reqi
// passive program. It is not an executing-skill modifier or a timed buff.
// Native: kind 4 (client +828), reqi -> server +3A0; the server contributes
// cr[0] to Param12 channel 0 after the equipment predicate succeeds.
type SkillPassiveCritical struct {
	Pinned     bool
	Flat       uint32
	WeaponKind uint8
}

func encodedPassiveCritical(fields []string) SkillPassiveCritical {
	var out SkillPassiveCritical
	// Column 8 is activity (client 7F9451 -> +6F5), distinct from
	// column 6's original-skill ID. Passive Param12 writer requires activity 0.
	if len(fields) <= skilldataColEncodedTail || fields[68] != "4" || fields[8] != "0" || fields[9] != "0" {
		return out
	}
	seenCritical, seenRequirement := false, false
	for i := skilldataColEncodedTail; i < len(fields); {
		tag, ok := textdataInt(fields[i])
		if !ok {
			return SkillPassiveCritical{}
		}
		if tag == 0 {
			i++
			continue
		}
		if i+2 >= len(fields) {
			return SkillPassiveCritical{}
		}
		a, aOK := textdataInt(fields[i+1])
		b, bOK := textdataInt(fields[i+2])
		if !aOK || !bOK || a < 0 || a > 0xffffffff || b < 0 || b > 0xffffffff {
			return SkillPassiveCritical{}
		}
		switch tag {
		case skillCriticalTag:
			// Percent, additional riders and duplicate blocks require their own
			// complete program admission; never partially activate a passive.
			if seenCritical || b != 0 {
				return SkillPassiveCritical{}
			}
			seenCritical = true
			out.Flat = uint32(a)
		case 0x72657169:
			// reqi 6,TID4 selects weapon slot 6; armor/shield requirements and
			// reqn AND/OR combinations are separate programs, not fallbacks.
			kind, valid := passiveWeaponRequirement(a, b)
			if seenRequirement || !valid {
				return SkillPassiveCritical{}
			}
			seenRequirement = true
			out.WeaponKind = kind
		default:
			return SkillPassiveCritical{}
		}
		i += 3
	}
	if !seenCritical || !seenRequirement {
		return SkillPassiveCritical{}
	}
	out.Pinned = true
	return out
}

// Native reqi 6,TID4 reads the equipped weapon, not a skill-name family.
// The combat projection excludes broken items before evaluating this predicate.
func passiveWeaponRequirement(slot, kind int64) (uint8, bool) {
	if slot != 6 || kind < 1 || kind > 31 {
		return 0, false
	}
	return uint8(kind), true
}
