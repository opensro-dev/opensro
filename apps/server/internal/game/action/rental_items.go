/*
===========================================================================

rental_items.go - expired rentals leave when their storage loads

Native deletes an out-of-period rental when its storage loads (the
server's _DelExpiredRentItemInStorage precedes the rent info on every
loader: bag, avatar rows, COS bags, the character warehouse and the
guild chest), so a rental whose PeriodEndTime has passed never survives
a load of the storage holding it (issue #4240). The expiry stamped on a
delivered row is inventory.Item.RentExpiresUnix. The character's own
rows load at world entry (bootstrap); the two warehouses list on open.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
)

/*
================
Runtime.RemoveExpiredRentItems

The entry cleanup the enterworld door calls: every rental whose expiry
the clock has reached leaves every container the character record
carries, the walk the event cleanup makes (removeEventItems). True only
when a row actually left, so the door writes nothing for anyone else.
================
*/
func (rt *Runtime) RemoveExpiredRentItems(character *enterworld.Character) bool {
	if character == nil {
		return false
	}
	return removeExpiredRentals(character, rt.Now().Unix())
}

/*
================
removeExpiredRentals

Every rented row whose expiry is at or past nowUnix leaves: the bag,
the avatar rows, the active companion, each mercenary, and the bags
that ride inside summon rows (a dismissed pet's bag).
================
*/
func removeExpiredRentals(character *enterworld.Character, nowUnix int64) bool {
	removed := removeExpiredRentRows(&character.MissionInventory, nowUnix)
	if character.AvatarInventory != nil && removeExpiredRentRows(&character.AvatarInventory.Rows, nowUnix) {
		removed = true
	}
	if removeExpiredRentRowsFromCOS(character.ActiveCOS, nowUnix) {
		removed = true
	}
	for _, mercenary := range character.Mercenaries {
		if removeExpiredRentRowsFromCOS(mercenary, nowUnix) {
			removed = true
		}
	}
	return removed
}

/*
================
removeExpiredRentRowsFromCOS

Sweeps one companion's bag. A nil companion or bagless one has nothing
to give up.
================
*/
func removeExpiredRentRowsFromCOS(pet *enterworld.CharacterCOS, nowUnix int64) bool {
	if pet == nil || pet.Container == nil {
		return false
	}
	return removeExpiredRentRows(&pet.Container.Rows, nowUnix)
}

/*
================
rentalRowsExpired

Whether any row carries a rental whose expiry the clock has reached.
The two warehouse list doors read first and transact only then, so
opening a warehouse with no expired rental writes nothing.
================
*/
func rentalRowsExpired(rows []enterworld.InventoryRow, nowUnix int64) bool {
	for _, row := range rows {
		if row.RentExpiresUnix > 0 && row.RentExpiresUnix <= nowUnix {
			return true
		}
		if row.Summon != nil && row.Summon.Container != nil && rentalRowsExpired(row.Summon.Container.Rows, nowUnix) {
			return true
		}
	}
	return false
}

/*
================
removeExpiredRentRows

Filters expired rentals out of one row list in place and follows each
kept summon row into the bag it carries. A row is a rental by its
expiry alone; zero is no rental, and a future expiry keeps the row.
================
*/
func removeExpiredRentRows(rows *[]enterworld.InventoryRow, nowUnix int64) bool {
	source := *rows
	kept := source[:0]
	removed := false
	for _, row := range source {
		if row.RentExpiresUnix > 0 && row.RentExpiresUnix <= nowUnix {
			removed = true
			continue
		}
		if removeExpiredRentRowsFromCOS(row.Summon, nowUnix) {
			removed = true
		}
		kept = append(kept, row)
	}
	for i := len(kept); i < len(source); i++ {
		source[i] = enterworld.InventoryRow{}
	}
	*rows = kept
	return removed
}
