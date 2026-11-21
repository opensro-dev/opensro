/*
===========================================================================

skillcodename.go - skill rows by codename

Items, events and transforms name their skill by its version-stable
codename (skilldata column 3) rather than its id, which an import may
renumber. This is the one place the runtime asks the skill table for it.

===========================================================================
*/

package action

import "opensro.online/server/internal/game/enterworld"

/*
================
skillByCodename

The row a codename names. False when the table has none, or when the
skill source answers no codename lookup at all (a test's id-only table).
================
*/
func (rt *Runtime) skillByCodename(codename string) (enterworld.SkillRow, bool) {
	source, ok := rt.deps.SkillData().(interface {
		SkillByCodename(string) (enterworld.SkillRow, bool)
	})
	if !ok || codename == "" {
		return enterworld.SkillRow{}, false
	}
	return source.SkillByCodename(codename)
}
