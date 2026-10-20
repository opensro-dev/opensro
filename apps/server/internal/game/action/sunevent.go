/*
===========================================================================

sunevent.go - degree-9 Seal of Sun gear and its two server rules

Port-only, not native. The drop rule (progression/sunevent.go,
SRO_SUN_EVENT_ONLY, off in deployments since 2026-10-20) is asked by
prepareSelectedDrop, where every monster drop ends: while it is on, a
non-unique monster's Sun pick drops nothing, while a unique kill keeps
its Sun pick. The shop rule is separate and permanent (owner
2026-10-20): ConfigureCommerce withholds every Sun offer from the NPC
shops whatever the drop rule says, through SunShopExcluded below.

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

/*
================
SunShopExcluded

Whether the permanent shop rule keeps this item off every NPC shelf:
the same degree-9 Sun identity as the drop rule, asked without the
drop rule's flag (owner 2026-10-20; action/commerce.go).
================
*/
func SunShopExcluded(ref *enterworld.ItemRef) bool {
	if ref == nil {
		return false
	}
	rarity, _ := ref.NativeFields.Lookup("rarity")
	itemClass, _ := ref.NativeFields.Lookup("itemClass")
	return progression.IsEventOnlySun(ref.TypeIDs, int(rarity), int(itemClass))
}
