/*
===========================================================================

skilltimedrecovery.go - the recovery rates of rows with no timed program

A fortress structure skill is no player program (its leading words are
not a player's), so CompileSkillProgram never reads its irgc block and
its recovery rates would reach no reader. This projection runs after
parseSkillTimedEffect and claims only a row that program left without
rates, leaving every row it claimed exactly as it stood.

===========================================================================
*/

package enterworld

/*
================
projectTimedRecoveryRates

Walk the encoded tail for the irgc block (tagTimedRecovery) and record
its two rates on the row's timed effect, where the buff installer
reads them. A row whose timed program already set rates keeps them.
================
*/
func projectTimedRecoveryRates(fields []string, row *SkillRow) {
	if row.TimedEffect.Recovery.Present {
		return
	}
	word := func(i int) (uint32, bool) {
		if i >= len(fields) {
			return 0, false
		}
		v, ok := textdataInt(fields[i])
		return uint32(v), ok && v >= 0 && v <= 0xffffffff
	}
	for i := skilldataColEncodedTail; i < len(fields); {
		tag, ok := textdataInt(fields[i])
		if !ok || tag == 0x73736f75 {
			return
		}
		if tag == 0 {
			i++
			continue
		}
		if tag == tagTimedRecovery {
			hp, hpOK := word(i + 1)
			mp, mpOK := word(i + 2)
			if hpOK && mpOK {
				row.TimedEffect.Recovery = SkillRecoveryRates{Present: true, HP: hp, MP: mp}
			}
			return
		}
		i += 1 + spawnParamArity(uint32(tag))
	}
}
