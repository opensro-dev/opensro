/*
===========================================================================

tick_snapshots.go - one shared record copy per character per tick

The tick's steps each used to clone the same character record through
the snapshot roots (runtime_position.go): at 105 players the repeated
read-door clones were about 10 ms of the tick (#3099). While a tick
hook runs, the roots below serve one copy per character and copy kind
from this cache instead of cloning again.

THE INVARIANT (this cache is sound only while all of it holds):

  - A copy is cached with the record's write epoch (store
    CharacterWriteEpoch), read before the clone is taken. The copy is
    served again only while the live record still reports that
    epoch, and every write to a record - immediate or deferred, from
    a tick step or a packet handler - passes a store door that bumps
    the epoch under the write lock before the mutation is visible.
    An unchanged epoch therefore proves a fresh clone would return
    the same bytes, so sharing never serves stale state; the first
    root after any write re-clones.
  - Copies stay read-only in every reader's hands, the contract the
    snapshot roots already state. Nothing writes into a copy a root
    returned; a reader that must change state writes the live record
    through a door (which bumps the epoch and retires the copy).
  - The cache lives only between beginTickSnapshots and
    endTickSnapshots on the tick hook. Outside the bracket - packet
    handlers included - every root clones exactly as before. Inside
    it, the mutex (not the goroutine) is what a concurrent handler
    root call shares through, under the same epoch proof.

A composition without the epoch wired (detached fixtures) reports
unwired from Deps.WriteEpoch and nothing is ever shared.

===========================================================================
*/

package action

import (
	"sync"

	"opensro.online/server/internal/game/enterworld"
)

// tickSnapshotKind names one snapshot root's copy shape. Kinds never
// share an entry: a combat copy drops bag rows a cast copy keeps, so
// a reader cleared for one shape must never be served another.
type tickSnapshotKind uint8

const (
	tickSnapshotFull tickSnapshotKind = iota
	tickSnapshotEquipment
	tickSnapshotCombat
	tickSnapshotCast
	tickSnapshotCompanion
	tickSnapshotKindCount
)

/*
================
tickSnapshotEntry

One cached copy and the write epoch the live record reported just
before the copy was cloned.
================
*/
type tickSnapshotEntry struct {
	epoch    uint64
	snapshot *enterworld.Character
}

/*
================
tickSnapshots

The tick's shared copies, one map per copy kind. mu guards active
and the maps: packet handlers reach the roots while a tick runs, and
the epoch proof - not the calling goroutine - is what makes a share
sound, so the maps must be safe for concurrent use.
================
*/
type tickSnapshots struct {
	mu     sync.Mutex
	active bool
	byKind [tickSnapshotKindCount]map[*enterworld.Character]tickSnapshotEntry
}

/*
================
beginTickSnapshots

Opens the cache for one tick hook. Called by TickHook and
MonsterActionTickHook before their first step.
================
*/
func (rt *Runtime) beginTickSnapshots() {
	rt.tickSnaps.mu.Lock()
	defer rt.tickSnaps.mu.Unlock()
	rt.tickSnaps.active = true
}

/*
================
endTickSnapshots

Closes the cache and drops every copy. Deferred by the tick hooks,
so a panicking step the ticker recovers still closes the bracket.
================
*/
func (rt *Runtime) endTickSnapshots() {
	rt.tickSnaps.mu.Lock()
	defer rt.tickSnaps.mu.Unlock()
	rt.tickSnaps.active = false
	for kind := range rt.tickSnaps.byKind {
		clear(rt.tickSnaps.byKind[kind])
	}
}

/*
================
tickSnapshot

The cached copy for the record and kind, when one is live. The
second result is the record's current write epoch, read before any
clone the caller is about to take: storing the fresh copy under that
epoch is sound because a write landing after the read moves the
epoch and retires the entry on the next lookup. ok is false outside
the tick bracket, without the epoch wired, and on any epoch move.
================
*/
func (rt *Runtime) tickSnapshot(kind tickSnapshotKind, character *enterworld.Character) (snapshot *enterworld.Character, epoch uint64, ok bool) {
	epoch, wired := rt.deps.WriteEpoch(character)
	if !wired {
		return nil, 0, false
	}
	rt.tickSnaps.mu.Lock()
	defer rt.tickSnaps.mu.Unlock()
	if !rt.tickSnaps.active {
		return nil, 0, false
	}
	entry, found := rt.tickSnaps.byKind[kind][character]
	if !found || entry.epoch != epoch {
		return nil, epoch, false
	}
	return entry.snapshot, epoch, true
}

/*
================
storeTickSnapshot

Keeps a fresh copy for the rest of the tick, under the epoch read
just before it was cloned (see tickSnapshot). A nil copy and a
closed bracket keep nothing.
================
*/
func (rt *Runtime) storeTickSnapshot(kind tickSnapshotKind, character *enterworld.Character, epoch uint64, snapshot *enterworld.Character) {
	if snapshot == nil {
		return
	}
	rt.tickSnaps.mu.Lock()
	defer rt.tickSnaps.mu.Unlock()
	if !rt.tickSnaps.active {
		return
	}
	copies := rt.tickSnaps.byKind[kind]
	if copies == nil {
		copies = make(map[*enterworld.Character]tickSnapshotEntry)
		rt.tickSnaps.byKind[kind] = copies
	}
	copies[character] = tickSnapshotEntry{epoch: epoch, snapshot: snapshot}
}
