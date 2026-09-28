/*
===========================================================================

equipmentresources.go - intrinsic equipment contributions to HP and MP

The combat projection owns equipment admission, including durability and
slot lifetime. This module supplies family-defined resource contributions
after that admission, independently of optional blue item attributes.

===========================================================================
*/

package combat

import "opensro.online/server/internal/game/paramkeeper"

const (
	weaponSubclassShift = 11
	harpWeaponSubclass  = 14
	maximumMPParameter  = 4
	harpMPPercent       = 50
)

/*
================
equipmentResourceWrites

Native 497C20..497C4D tests the weapon subclass and inserts +50 into max MP's
percentage-sum channel, owned by the item. This is independent of degree,
variance, plus, codename and learned Bard skills. Ordinary keeper rebuilding
therefore removes it when the equipment disappears or loses durability.
================
*/
func equipmentResourceWrites(tid uint16, source uint32) []paramkeeper.Write {
	if !isWeaponFamily(tid) || tid>>weaponSubclassShift != harpWeaponSubclass {
		return nil
	}
	return []paramkeeper.Write{{
		Parameter: maximumMPParameter,
		Channel:   paramkeeper.PercentSum,
		Source:    source,
		Value:     harpMPPercent,
	}}
}
