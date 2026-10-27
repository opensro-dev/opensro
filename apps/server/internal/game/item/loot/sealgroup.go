/*
===========================================================================

sealgroup.go - the rare class of one degree and seal at a drop level

A unique's guaranteed seal is never a random class: it is the highest
degree the level's ordinary class row offers, in one named seal tier. The
rare table interleaves three seal classes per degree (G = 3*(D-1) + tier,
Star, Moon, Sun), the same triple the ordinary row counts its strength
tiers in (equipment.go, equipmentClassStep). Port-only use: the unique
seal reward (action/monsterunique.go, owner 2026-10-25).

===========================================================================
*/
package loot

/*
================
SealGroupAt

The rare class group of the highest degree this level offers (the level
row's own first class, SpecialEquipmentGroup) and one seal tier: 0 Star,
1 Moon, 2 Sun. SelectEquipment still walks down by one degree at a time
when that degree has no bucket, so a catalog without the top degree's rare
row keeps the seal and drops the degree below it.
================
*/
func SealGroupAt(level uint8, tier int) (int, bool) {
	group, ok := SpecialEquipmentGroup(level)
	if !ok {
		return 0, false
	}
	seals := equipmentClassStep(true)
	if tier < 0 || tier >= seals {
		return 0, false
	}
	rare := group/seals*seals + tier
	if rare >= equipment.widths[1] {
		return 0, false
	}
	return rare, true
}
