/*
===========================================================================

mercenary.go - the authored guild-soldier level lookup

The base scroll reference selects a characterdata group, whose ordered
level rows are the native reference vector. Never manufacture codenames.

===========================================================================
*/
package enterworld

import "opensro.online/server/internal/domain"

/*
================
MercenaryReference

4A2CE0 checks vector size strictly greater than player level, then indexes
level-1. Preserve its upper-bound refusal even though the final row exists.
================
*/
func MercenaryReference(source CharacterRefSource, base string, level int64) (*CharacterRef, bool) {
	if source == nil || level < 1 || level > 255 {
		return nil, false
	}
	first, valid := source.CharacterRefByCodename(base)
	if !valid || first == nil || first.TidWord != 0x1c6|domain.MercenaryBand<<11 || first.GroupCodename == "" {
		return nil, false
	}
	count := 0
	var selected *CharacterRef
	for _, ref := range source.SummonableCharacterRefs() {
		if ref.TidWord != first.TidWord || ref.GroupCodename != first.GroupCodename {
			continue
		}
		count++
		if int64(ref.Level) == level {
			if selected != nil {
				return nil, false
			}
			copy := ref
			selected = &copy
		}
	}
	return selected, selected != nil && int64(count) > level
}
