/*
===========================================================================

skillreplacementrow.go - feeds a skill row to the replacement compiler

compileSkillReplacement (skillreplacement.go) is sha256-pinned to the
native corpus and reads unsigned words only. Later tables write column 18
(Action_Overlap) as a signed DWORD, so a word with the top bit set arrives
negative. This file owns the one adapter that turns that text back into the
unsigned word before the pinned compiler sees it.

===========================================================================
*/

package enterworld

import (
	"strconv"

	"opensro.online/server/internal/game/item/statuseffect"
)

// skilldataColActionOverlap is Action_Overlap: the cooldown group in its top
// byte and the overlap rule below it.
const skilldataColActionOverlap = 18

/*
================
compileSkillReplacementRow

Normalize a signed 32-bit Action_Overlap word before compileSkillReplacement.
Later tables write negative signed DWORDs there (such as -872415232 for
0xCC000000 on Monster Defence Ignore). A value below INT32_MIN is left as it
is, so the compiler still refuses it.
================
*/
func compileSkillReplacementRow(fields []string) (statuseffect.ReplacementDescriptor, error) {
	if len(fields) > skilldataColActionOverlap {
		if v, ok := textdataInt(fields[skilldataColActionOverlap]); ok && v < 0 && v >= -0x80000000 {
			normalized := make([]string, len(fields))
			copy(normalized, fields)
			normalized[skilldataColActionOverlap] = strconv.FormatUint(uint64(uint32(v)), 10)
			return compileSkillReplacement(normalized)
		}
	}
	return compileSkillReplacement(fields)
}
