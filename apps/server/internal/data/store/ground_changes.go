/*
===========================================================================

ground_changes.go - the commit persists only the ground rows that changed

The registry stamps every gid a mutation touches (grounditem
changes.go). At the commit door the store takes those rows, writes them
inside the commit's transaction, and only once the transaction has
committed applies them to loadedRecords and acknowledges the revision
it read. A rolled-back commit retains and acknowledges nothing, so the
next commit takes the same rows again (#2040).

When the registry cannot say what changed (its first commit, a
Restore) it reports Full, and the commit diffs the whole plane against
loadedRecords as before (writeGroundDiffTx).

===========================================================================
*/
package store

import (
	"cmp"
	"database/sql"
	"fmt"
	"slices"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/platform/jsonscratch"
)

/*
================
takeGroundChangesLocked

The ground rows this commit must write, or nil when the revision has
not moved since the last committed ground write. A store that has
never recorded a revision diffs the whole plane, whatever the registry
reports. Caller holds s.mu.
================
*/
func (s *Store) takeGroundChangesLocked() *domain.GroundChanges {
	if s.ground.source == nil {
		return nil
	}
	if s.ground.revisionRecorded && s.ground.source.Revision() == s.ground.committedRev {
		return nil
	}
	changes := s.ground.source.Changes()
	if !s.ground.revisionRecorded && !changes.Full {
		// Read after the revision: the snapshot is at least as new as
		// changes.Revision, so acknowledging that revision drops no
		// stamp the snapshot has not written.
		changes.Full = true
		changes.Snapshot = s.ground.source.Snapshot()
		changes.Rows = nil
	}
	return &changes
}

/*
================
writeGroundChangesTx

Writes the changes inside the commit's transaction: the whole-plane
diff for a Full set, else one statement per changed row. The GidCounter
is written with every ground write, so a moved counter persists even
when no item row changed.
================
*/
func writeGroundChangesTx(tx *sql.Tx, scratch *jsonscratch.Scratch, committed map[string][]domain.GroundItemRecord, changes *domain.GroundChanges) error {
	if changes.Full {
		if _, _, err := writeGroundDiffTx(tx, scratch, committed, changes.Snapshot.Divisions); err != nil {
			return err
		}
	} else {
		for i := range changes.Rows {
			row := &changes.Rows[i]
			if row.Record == nil {
				if _, err := tx.Exec(deleteGroundSQL, row.Division, row.Gid); err != nil {
					return err
				}
				continue
			}
			if err := upsertGroundTx(tx, scratch, row.Division, row.Record); err != nil {
				return err
			}
		}
	}
	return upsertMetaTx(tx, metaKeyGidCounter, fmt.Sprintf("%d", changes.GidCounter))
}

/*
================
retainGroundChangesLocked

Called only after the commit's transaction has committed: loadedRecords
becomes what the table now holds, the revision is recorded, and the
registry may drop the stamps the commit wrote. Caller holds s.mu.
================
*/
func (s *Store) retainGroundChangesLocked(changes *domain.GroundChanges) {
	if changes.Full {
		s.ground.loadedRecords = changes.Snapshot.Divisions
	} else {
		applyGroundChanges(s.ground.loadedRecords, changes.Rows)
	}
	s.ground.loadedGidCounter = changes.GidCounter
	s.ground.committedRev = changes.Revision
	s.ground.revisionRecorded = true
	s.ground.source.Acknowledge(changes.Revision)
}

/*
================
applyGroundChanges

Applies committed rows to the gid-sorted plane in place: an upsert
replaces or inserts its row, a delete removes it, and a division left
empty is dropped (the snapshot shape omits empty divisions).
================
*/
func applyGroundChanges(plane map[string][]domain.GroundItemRecord, rows []domain.GroundChange) {
	byGid := func(row domain.GroundItemRecord, gid uint32) int { return cmp.Compare(row.Gid, gid) }
	for i := range rows {
		change := &rows[i]
		division := plane[change.Division]
		at, found := slices.BinarySearchFunc(division, change.Gid, byGid)
		switch {
		case change.Record == nil && found:
			division = slices.Delete(division, at, at+1)
		case change.Record != nil && found:
			division[at] = *change.Record
		case change.Record != nil:
			division = slices.Insert(division, at, *change.Record)
		}
		if len(division) == 0 {
			delete(plane, change.Division)
			continue
		}
		plane[change.Division] = division
	}
}
