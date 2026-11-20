/*
===========================================================================

item_identity_rebind.go - stored items follow their codename's id at boot

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
missing-reference handling owns it. Companions' own character ids are not
rebound; no build has moved one.

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
rebindSwap

The swap that gives a row its codename's current id.
================
*/
func rebindSwap(identity ItemIdentity) ItemSwapFunc {
	return func(row domain.InventoryRow) (domain.InventoryRow, bool, error) {
		next, changed := rebindRow(row, identity)
		return next, changed, nil
	}
}

/*
================
rebindRow
================
*/
func rebindRow(row domain.InventoryRow, identity ItemIdentity) (domain.InventoryRow, bool) {
	if row.Codename == "" {
		return row, false
	}
	id, known := identity(row.Codename)
	if !known || id == 0 || id == row.RefObjID {
		return row, false
	}
	row.RefObjID = id
	return row, true
}

/*
================
RebindItemIdentities

Rebinds every stored item of division and its market listings; returns
how many rows moved. One transaction per part: the containers commit
together, then the listings.
================
*/
func (s *Store) RebindItemIdentities(division string, identity ItemIdentity) (int, error) {
	swaps, err := s.SwapItemsOfflineShard(division, rebindSwap(identity), true)
	if err != nil {
		return 0, fmt.Errorf("rebind stored items: %w", err)
	}
	listings, err := s.rebindListings(division, identity)
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
func (s *Store) rebindListings(division string, identity ItemIdentity) (int, error) {
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
		next, ok := rebindRow(item, identity)
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
