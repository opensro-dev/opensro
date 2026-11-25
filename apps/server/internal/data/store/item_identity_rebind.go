/*
===========================================================================

item_identity_rebind.go - stored objects follow their codename's id at boot

A stored item row keeps both its reference id and its codename. The id is
what the wire and most lookups use; the codename is the stable name. A
build that numbers an object afresh (the gears magic stones once held the
fox pets' ids and moved off them, #3994) leaves every stored row of that
item under its old id, which now names another object or none.

RebindItemIdentities runs once at GameWorld boot, before any session: it
gives every stored row whose codename the catalogue knows that codename's
current id. It covers every container the offline shard swap walks (bags,
worn and avatar sockets, companions, the arena chest, warehouses, guild
storage, the ground plane) and the market's open and held listings. A row
whose codename the catalogue does not know is left as it is: the existing
missing-reference handling owns it.

A stored companion (a pet or a ride kept in its scroll row's summon) names
its own character the same way, and the same build moved one: #3995 kept
the clashing id with the stone and numbered COS_P_FOX_001 afresh (50853 to
51295). A fox summoned before that release still named the stone, so its
owner's entry was refused (invalidCOSRestoration, #4019). The companion
takes its character codename's current id as well.

===========================================================================
*/
package store

import (
	"encoding/json"
	"fmt"

	"opensro.online/server/internal/domain"
)

/*
================
ItemIdentity

The catalogue's current reference id for an item codename.
================
*/
type ItemIdentity func(codename string) (uint32, bool)

/*
================
ObjectIdentities

The two catalogues a stored row can name: its own item, and the character
of the companion it carries. A nil Companion leaves companions alone.
================
*/
type ObjectIdentities struct {
	Item      ItemIdentity
	Companion ItemIdentity
}

/*
================
rebindSwap

The swap that gives a row its codename's current id.
================
*/
func rebindSwap(identities ObjectIdentities) ItemSwapFunc {
	return func(row domain.InventoryRow) (domain.InventoryRow, bool, error) {
		next, changed := rebindRow(row, identities)
		return next, changed, nil
	}
}

/*
================
rebindRow

The row's own item id, then its companion's character id. The companion
is copied before it changes: the row's summon is shared with the caller's
copy of the row.
================
*/
func rebindRow(row domain.InventoryRow, identities ObjectIdentities) (domain.InventoryRow, bool) {
	changed := false
	if id, moved := currentID(identities.Item, row.Codename, row.RefObjID); moved {
		row.RefObjID = id
		changed = true
	}
	if row.Summon != nil {
		if id, moved := currentID(identities.Companion, row.Summon.Codename, row.Summon.RefObjID); moved {
			summon := *row.Summon
			summon.RefObjID = id
			row.Summon = &summon
			changed = true
		}
	}
	return row, changed
}

/*
================
currentID

The codename's current id when it differs from stored; false for an
empty codename, one the catalogue does not know, or no catalogue.
================
*/
func currentID(identity ItemIdentity, codename string, stored uint32) (uint32, bool) {
	if identity == nil || codename == "" {
		return 0, false
	}
	id, known := identity(codename)
	if !known || id == 0 || id == stored {
		return 0, false
	}
	return id, true
}

/*
================
RebindItemIdentities

Rebinds every stored item and companion of division and its market
listings; returns how many rows moved. One transaction per part: the containers commit
together, then the listings.
================
*/
func (s *Store) RebindItemIdentities(division string, identities ObjectIdentities) (int, error) {
	swaps, err := s.SwapItemsOfflineShard(division, rebindSwap(identities), true)
	if err != nil {
		return 0, fmt.Errorf("rebind stored items: %w", err)
	}
	listings, err := s.rebindListings(division, identities)
	if err != nil {
		return len(swaps), fmt.Errorf("rebind market listings: %w", err)
	}
	return len(swaps) + listings, nil
}

/*
================
rebindListings

The open and held listings of division: their item is in escrow, outside
every container. Sold and returned rows are history and keep what they
recorded.
================
*/
func (s *Store) rebindListings(division string, identities ObjectIdentities) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	db := s.syncDBLocked()
	rows, err := db.Query("SELECT id, item FROM market_listings WHERE division = ? AND state IN ('open', 'held')", division)
	if err != nil {
		return 0, err
	}
	type moved struct {
		id   int64
		item []byte
	}
	var changed []moved
	for rows.Next() {
		var id int64
		var raw string
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return 0, err
		}
		var item domain.InventoryRow
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			rows.Close()
			return 0, fmt.Errorf("listing %d: %w", id, err)
		}
		next, ok := rebindRow(item, identities)
		if !ok {
			continue
		}
		encoded, err := json.Marshal(next)
		if err != nil {
			rows.Close()
			return 0, err
		}
		changed = append(changed, moved{id, encoded})
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(changed) == 0 {
		return 0, nil
	}
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	for _, listing := range changed {
		if _, err := tx.Exec("UPDATE market_listings SET item = ? WHERE id = ?", string(listing.item), listing.id); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(changed), nil
}
