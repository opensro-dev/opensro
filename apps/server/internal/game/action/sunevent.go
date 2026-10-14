/*
===========================================================================

sunevent.go - degree-9 Seal of Sun gear leaves drops and shops

Port-only, not native (progression/sunevent.go, SRO_SUN_EVENT_ONLY). The
runtime's two Sun sources each ask SunExcludes: prepareSelectedDrop,
where every monster drop ends (a Sun pick drops nothing), and
ConfigureCommerce, which withholds every Sun offer from the NPC shops.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/progression"
)

/*
================
SunExcludes

Whether the Sun rule keeps this item from drops and shops. The catalogue
tool asks the same question for the website's tables.
================
*/
func SunExcludes(rule progression.SunEventOnly, ref *enterworld.ItemRef) bool {
	if ref == nil {
		return false
	}
	rarity, _ := ref.NativeFields.Lookup("rarity")
	itemClass, _ := ref.NativeFields.Lookup("itemClass")
	return rule.Excludes(ref.TypeIDs, int(rarity), int(itemClass))
}
