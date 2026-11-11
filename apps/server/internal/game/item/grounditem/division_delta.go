/*
===========================================================================

division_delta.go - which drops of one division changed since a reader's read

The ground scope pass keeps a copy of every division's drops (action
ground_scope.go). It used to take that copy whole, with All, every time
the division's revision moved: one drop landing or one pickup copied
every drop of the division again. On the live shard that was 6% of all
bytes the server allocated (#3164: 0.25 TB of 4.07 TB in ten hours).

The registry already knows what each mutation touched: every mutator
stamps its gids for the store's commit (changes.go). This file keeps a
second, bounded note of the same stamps per division, in division
revisions, for a reader that holds a copy and wants only what moved:
DivisionChangesSince answers with the changed rows and the removed
gids, read atomically with the revision they bring the copy to.

The log is a convenience, never the truth. It is bounded
(divisionLogLimit); a reader further behind than it reaches, or one
that read before a Restore, is told so and takes DivisionSnapshot
instead. The store's journal is untouched: it is acknowledged by the
commit and cannot serve a second reader.

===========================================================================
*/
package grounditem

import (
	"slices"
	"sort"

	"opensro.online/server/internal/domain"
)

// divisionLogLimit is how many stamps one division's log keeps. A busy
// division sees a few drops a tick and its reader reads every tick, so
// a reader inside the log is the rule; past it the reader pays one full
// copy, as it always did.
const divisionLogLimit = 512

/*
================
divisionLogEntry, divisionLog

One stamp: gid changed at the division's revision. entries are in
revision order. floor is the newest revision the log no longer lists
in full: a reader at or past it is served, one before it is not.
================
*/
type divisionLogEntry struct {
	revision uint64
	gid      uint32
}

type divisionLog struct {
	entries []divisionLogEntry
	floor   uint64
}

/*
================
DivisionDelta

What changed in one division after a reader's revision: the rows that
changed and are present, gid ascending and deep-copied as All copies
them, the gids that are gone, ascending, and the revision this brings
the reader to.
================
*/
type DivisionDelta struct {
	Revision uint64
	Changed  []Item
	Removed  []uint32
}

/*
================
noteDivisionChangeLocked

Records gid in its division's log at the division's current revision.
stampLocked calls it, so every mutator that stamps for the store also
notes here. A log past its limit drops its older half and raises the
floor to the last revision it dropped. Caller holds r.mu.
================
*/
func (r *Registry) noteDivisionChangeLocked(divisionID string, gid uint32) {
	if r.divLogs == nil {
		r.divLogs = make(map[string]*divisionLog)
	}
	log := r.divLogs[divisionID]
	if log == nil {
		log = &divisionLog{}
		r.divLogs[divisionID] = log
	}
	log.entries = append(log.entries, divisionLogEntry{revision: r.divRevisions[divisionID], gid: gid})
	if len(log.entries) <= divisionLogLimit {
		return
	}
	cut := len(log.entries) - divisionLogLimit/2
	log.floor = log.entries[cut-1].revision
	log.entries = append(log.entries[:0], log.entries[cut:]...)
}

/*
================
forgetDivisionLogsLocked

The plane was replaced wholesale (Restore): no log describes the step
from any earlier revision, so every reader that read before now takes
a full copy. Caller holds r.mu, after the revision moved.
================
*/
func (r *Registry) forgetDivisionLogsLocked() {
	r.divLogs = nil
	r.divLogFloor = r.revision
}

/*
================
DivisionChangesSince

The rows of divisionID that changed after the division's revision
since, read atomically with the revision they lead to. ok is false
when the log cannot answer - since is older than the log reaches,
predates a Restore, or is not a revision this registry issued - and
the reader takes DivisionSnapshot. An unchanged division answers ok
with an empty delta at since.
================
*/
func (r *Registry) DivisionChangesSince(divisionID string, since uint64) (DivisionDelta, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	current := r.divRevisions[divisionID]
	if since > current || since < r.divLogFloor {
		return DivisionDelta{}, false
	}
	delta := DivisionDelta{Revision: current}
	if since == current {
		return delta, true
	}
	log := r.divLogs[divisionID]
	if log == nil || since < log.floor {
		return DivisionDelta{}, false
	}
	first := sort.Search(len(log.entries), func(i int) bool { return log.entries[i].revision > since })
	gids := make([]uint32, 0, len(log.entries)-first)
	for _, entry := range log.entries[first:] {
		gids = append(gids, entry.gid)
	}
	slices.Sort(gids)
	gids = slices.Compact(gids)
	items := r.byDivision[divisionID]
	for _, gid := range gids {
		if item, present := items[gid]; present {
			delta.Changed = append(delta.Changed, cloneItem(item))
		} else {
			delta.Removed = append(delta.Removed, gid)
		}
	}
	return delta, true
}

/*
================
DivisionSnapshot

All, with the division revision the copy is exact at, read under one
lock: the reader's starting point for DivisionChangesSince.
================
*/
func (r *Registry) DivisionSnapshot(divisionID string) ([]Item, uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.allLocked(divisionID), r.divRevisions[divisionID]
}

/*
================
allLocked

The division's entries ordered by entity id, deep-copied. Caller
holds r.mu.
================
*/
func (r *Registry) allLocked(divisionID string) []Item {
	items := r.byDivision[divisionID]
	out := make([]Item, 0, len(items))
	for _, gid := range sortedGids(items) {
		out = append(out, cloneItem(items[gid]))
	}
	return out
}

/*
================
cloneItem

A registry row as a reader may keep it: its option list and summon
are its own.
================
*/
func cloneItem(item Item) Item {
	item.MagicOptions = append([]uint64(nil), item.MagicOptions...)
	item.Summon = domain.CloneCOS(item.Summon)
	return item
}
