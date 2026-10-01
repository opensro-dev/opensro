/*
===========================================================================

persist.go - owns persist behavior and its checked data boundaries

===========================================================================
*/
package grounditem

// Ground snapshot conversion: live registry entries to neutral domain values.
//
// Ground items persist inside the authority database through the store's
// commit door, so a drop/pickup and its character mutation land in one
// transaction. Snapshot and Restore convert between live registry entries and
// the domain-owned persistence values.

import (
	"sort"
	"strconv"
	"time"

	"opensro.online/server/internal/domain"
)

/*
================
persistedFromItem
================
*/
func persistedFromItem(item Item) domain.GroundItemRecord {
	row := domain.GroundItemRecord{
		RecordID:          item.RecordID,
		PopulationWorld:   item.Population.World,
		MagicOptions:      append([]uint64(nil), item.MagicOptions...),
		TransformRefObjID: item.TransformRefObjID, Summon: domain.CloneCOS(item.Summon),
		Gid:        item.Gid,
		RefObjID:   item.RefObjID,
		Codename:   item.Codename,
		TypeFlags:  item.TypeFlags,
		GoldAmount: item.GoldAmount,
		Plus:       item.Plus,
		Durability: item.Durability,
		StackCount: item.StackCount,
		RegionID:   item.Position.RegionID,
		X:          item.Position.X,
		Y:          item.Y,
		Z:          item.Position.Z,
		Heading:    item.Heading,
		OwnerJID:   item.OwnerJID,
		DroppedBy:  item.DroppedBy,
	}
	if item.VarianceBits != 0 {
		row.VarianceBits = strconv.FormatUint(item.VarianceBits, 10)
	}
	if !item.DroppedAt.IsZero() {
		row.DroppedAtMs = item.DroppedAt.UnixMilli()
	}
	return row
}

/*
================
itemFromPersisted
================
*/
func itemFromPersisted(row domain.GroundItemRecord) Item {
	// An unparseable variance re-arms as 0, matching the character-row
	// bridging (action invItemsFromRows).
	variance, err := strconv.ParseUint(row.VarianceBits, 10, 64)
	if err != nil {
		variance = 0
	}
	item := Item{
		RecordID:          row.RecordID,
		MagicOptions:      append([]uint64(nil), row.MagicOptions...),
		TransformRefObjID: row.TransformRefObjID, Summon: domain.CloneCOS(row.Summon),
		Gid:          row.Gid,
		RefObjID:     row.RefObjID,
		Codename:     row.Codename,
		TypeFlags:    row.TypeFlags,
		GoldAmount:   row.GoldAmount,
		Plus:         row.Plus,
		VarianceBits: variance,
		Durability:   row.Durability,
		StackCount:   row.StackCount,
		Position:     Point{RegionID: row.RegionID, X: row.X, Z: row.Z},
		Y:            row.Y,
		Heading:      row.Heading,
		OwnerJID:     row.OwnerJID,
		DroppedBy:    row.DroppedBy,
	}
	// The ORIGINAL drop timestamp comes back so the fixture TTL continues
	// across a restart as if it never happened (witnessed red: without this
	// line, rehydrated drops never expire - TestGroundStateTTLContinuesAcrossReboot).
	if row.DroppedAtMs != 0 {
		item.DroppedAt = time.UnixMilli(row.DroppedAtMs)
	}
	return item
}

// Snapshot returns a deep value copy of the whole registry, ordered
// deterministically (items by gid) so the state file is diff-stable.
/*
================
Snapshot
================
*/
func (r *Registry) Snapshot() domain.GroundSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.snapshotLocked()
}

// snapshotLocked builds the Snapshot. Callers hold the lock.
/*
================
snapshotLocked
================
*/
func (r *Registry) snapshotLocked() domain.GroundSnapshot {
	divisions := make(map[string][]domain.GroundItemRecord, len(r.byDivision))
	for divisionID, items := range r.byDivision {
		if len(items) == 0 {
			continue
		}
		rows := make([]domain.GroundItemRecord, 0, len(items))
		for _, item := range items {
			rows = append(rows, persistedFromItem(item))
		}
		sort.Slice(rows, func(a, b int) bool { return rows[a].Gid < rows[b].Gid })
		divisions[divisionID] = rows
	}
	return domain.GroundSnapshot{Version: domain.GroundSnapshotVersion, GidCounter: r.counter, Divisions: divisions}
}

// Restore replaces the registry's contents with the snapshot's: items come
// back with their ORIGINAL drop timestamps (the TTL continues across a
// restart as if it never happened) and the gid counter resumes where it
// left off. As a guard against a stale counter in a hand-edited file, the
// counter is bumped past every restored gid - a live gid must never be
// handed to a second drop. Boot rehydration is not a mutation: nothing
// is committed by a Restore.
/*
================
Restore
================
*/
func (r *Registry) Restore(s domain.GroundSnapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.byDivision = make(map[string]map[uint32]Item, len(s.Divisions))
	r.counter = s.GidCounter
	if r.counter > domain.MaxGroundItemGIDCounter {
		r.counter = domain.MaxGroundItemGIDCounter
	}
	r.revision++
	for divisionID, rows := range s.Divisions {
		items := make(map[uint32]Item, len(rows))
		for _, row := range rows {
			item := itemFromPersisted(row)
			if item.Gid <= GidBase || item.Gid > domain.GroundItemGIDLimit {
				// A gid-less row cannot be keyed, rendered or picked;
				// an out-of-band row would alias another entity plane.
				continue
			}
			if item.Gid > GidBase && item.Gid-GidBase > r.counter {
				r.counter = item.Gid - GidBase
			}
			// Keep the allocation watermark even for retired objects, but never
			// resurrect them into a new process-local population lifetime.
			if row.PopulationWorld == 0 {
				items[item.Gid] = item
			}
		}
		if len(items) > 0 {
			r.byDivision[divisionID] = items
		}
	}
}
