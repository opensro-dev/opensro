/*
===========================================================================

changes.go - which ground rows changed since the store last persisted

The authority store persists the ground plane at its commit door. It
used to take a full Snapshot and diff every row against the last
committed one, to find the one or two drops a busy commit changed
(#2040: a third of the synthetic shard's allocation). The registry now
records what it changed: every mutator stamps each gid it touches with
the revision of that mutation, and the store asks only for those rows.

The discipline is the ground plane's existing one (store
persistence.go): the store retains nothing until its transaction has
committed. Changes never clears a stamp; Acknowledge, called only
after the commit is durable, drops the stamps at or below the revision
that commit read. A failed commit acknowledges nothing, so the next
one takes the same rows again; a mutation racing the commit is stamped
above that revision and survives the acknowledgement. Rewriting a row
that is already durable (a crash between the commit and the
acknowledgement) writes the same bytes again, which is harmless.

When the change set is unknown - a fresh registry, a Restore - the
journal reports Full and the store diffs the whole plane, as before.

===========================================================================
*/
package grounditem

import (
	"cmp"
	"slices"
	"strings"

	"opensro.online/server/internal/domain"
)

/*
================
changeJournal

stamps holds, per division, each changed gid and the revision of its
latest change. full is set while the change set is unknown; fullAt is
the revision that made it so, cleared by an acknowledgement at or past
it. Owned by Registry, under its lock.
================
*/
type changeJournal struct {
	stamps map[string]map[uint32]uint64
	full   bool
	fullAt uint64
}

/*
================
stampLocked

Records that gid changed at the registry's current revision. Every
mutator calls it after bumping the revision. Caller holds r.mu.
================
*/
func (r *Registry) stampLocked(divisionID string, gid uint32) {
	if r.changes.stamps == nil {
		r.changes.stamps = make(map[string]map[uint32]uint64)
	}
	division := r.changes.stamps[divisionID]
	if division == nil {
		division = make(map[uint32]uint64)
		r.changes.stamps[divisionID] = division
	}
	division[gid] = r.revision
}

/*
================
resyncLocked

The change set is unknown from here on: the next Changes reports the
whole plane. Caller holds r.mu.
================
*/
func (r *Registry) resyncLocked() {
	r.changes.full = true
	r.changes.fullAt = r.revision
}

/*
================
Changes

What the store must write to bring the disk to this revision, read
atomically under the registry lock. It never clears a stamp: only
Acknowledge does, once the store's transaction has committed.
================
*/
func (r *Registry) Changes() domain.GroundChanges {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := domain.GroundChanges{Revision: r.revision, GidCounter: r.counter}
	if r.changes.full {
		out.Full = true
		out.Snapshot = r.snapshotLocked()
		return out
	}
	for divisionID, stamped := range r.changes.stamps {
		items := r.byDivision[divisionID]
		for gid := range stamped {
			change := domain.GroundChange{Division: divisionID, Gid: gid}
			if item, ok := items[gid]; ok {
				record := persistedFromItem(item)
				change.Record = &record
			}
			out.Rows = append(out.Rows, change)
		}
	}
	slices.SortFunc(out.Rows, func(a, b domain.GroundChange) int {
		if c := strings.Compare(a.Division, b.Division); c != 0 {
			return c
		}
		return cmp.Compare(a.Gid, b.Gid)
	})
	return out
}

/*
================
Acknowledge

The store's transaction for Changes at revision has committed: the
stamps at or below it are durable. A gid changed again after that
read keeps its newer stamp.
================
*/
func (r *Registry) Acknowledge(revision uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.changes.full && r.changes.fullAt <= revision {
		r.changes.full = false
	}
	for divisionID, stamped := range r.changes.stamps {
		for gid, at := range stamped {
			if at <= revision {
				delete(stamped, gid)
			}
		}
		if len(stamped) == 0 {
			delete(r.changes.stamps, divisionID)
		}
	}
}
