/*
===========================================================================

pkrecord_test.go - the PK clock sweep through the real store

A due decay drops the total by one, commits in the tick's batch and
survives a restart; a clock not yet due changes and commits nothing; an
empty record leaves the sweep. World entry repairs and decays through the
immediate door, because its handler answers after the commit.

===========================================================================
*/

package action

import (
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

const (
	pkTestTotal     = 3
	pkTestDecayWait = time.Hour
)

/*
================
pkStoreRuntime

The door runtime with the tick-batched door wired and one character whose
total decays in pkTestDecayWait.
================
*/
func pkStoreRuntime(t *testing.T) *doorRuntime {
	t.Helper()
	d := openDoorRuntime(t, filepath.Join(t.TempDir(), "authority"), testCharacter())
	deps := d.rt.deps.(*enterworld.Deps)
	deps.UpdateCharacterDeferred = d.authority.UpdateCharacterDeferred
	deps.ReadCharacter = func(_ string, read func()) { d.authority.ReadState(read) }
	d.authority.MutateCharacter(d.character, "seed-pk", func() {
		d.character.PK = &domain.PKRecord{TotalCount: pkTestTotal, TotalDecayAt: d.clock.Now().Add(pkTestDecayWait).Unix()}
	})
	return d
}

/*
================
pkSweep

One sweep inside an open tick batch: the commits counted before the flush
(immediate doors) and after it (the batch).
================
*/
func pkSweep(d *doorRuntime) (beforeFlush, afterFlush uint64) {
	start := d.authority.Health().Commits
	d.authority.BeginTickBatch()
	d.rt.advancePKRecords()
	beforeFlush = d.authority.Health().Commits - start
	d.authority.FlushTickBatch()
	return beforeFlush, d.authority.Health().Commits - start
}

/*
================
TestPKSweepDecaysADueTotalInTheTickBatch
================
*/
func TestPKSweepDecaysADueTotalInTheTickBatch(t *testing.T) {
	d := pkStoreRuntime(t)
	d.rt.pkOwners.track(testDivision, d.character.Name)

	if before, after := pkSweep(d); before != 0 || after != 0 {
		t.Fatalf("a clock not yet due committed (%d before the flush, %d after)", before, after)
	}
	if d.character.PK.TotalCount != pkTestTotal {
		t.Fatalf("total %d before its decay, want %d", d.character.PK.TotalCount, pkTestTotal)
	}

	d.clock.Advance(pkTestDecayWait)
	before, after := pkSweep(d)
	if before != 0 || after != 1 {
		t.Fatalf("the due decay committed %d before the flush and %d after, want only the batch", before, after)
	}
	if d.character.PK.TotalCount != pkTestTotal-1 {
		t.Fatalf("total %d after its decay, want %d", d.character.PK.TotalCount, pkTestTotal-1)
	}
	if keys := d.rt.pkOwners.keys(); len(keys) != 1 {
		t.Fatalf("tracked owners %v, want the record still running", keys)
	}

	reopened := d.reboot(t)
	if got := reopened.character.PK; got == nil || got.TotalCount != pkTestTotal-1 {
		t.Fatalf("persisted record %+v, want total %d after a restart", got, pkTestTotal-1)
	}
}

/*
================
TestPKSweepForgetsAnEmptyRecord
================
*/
func TestPKSweepForgetsAnEmptyRecord(t *testing.T) {
	d := pkStoreRuntime(t)
	d.authority.MutateCharacter(d.character, "clear-pk", func() { d.character.PK = &domain.PKRecord{} })
	d.rt.pkOwners.track(testDivision, d.character.Name)
	if before, after := pkSweep(d); before != 0 || after != 0 {
		t.Fatalf("an empty record committed (%d, %d)", before, after)
	}
	if keys := d.rt.pkOwners.keys(); len(keys) != 0 {
		t.Fatalf("tracked owners %v, want the empty record forgotten", keys)
	}
}

/*
================
TestPKEntryDecaysThroughTheImmediateDoor

The entry handler's decay commits at once, even inside an open batch.
================
*/
func TestPKEntryDecaysThroughTheImmediateDoor(t *testing.T) {
	d := pkStoreRuntime(t)
	d.clock.Advance(pkTestDecayWait)
	start := d.authority.Health().Commits
	d.authority.BeginTickBatch()
	d.rt.restorePKRecord(testDivision, d.character.Name)
	if got := d.authority.Health().Commits - start; got != 1 {
		t.Fatalf("entry committed %d times before the batch flushed, want its own immediate commit", got)
	}
	d.authority.FlushTickBatch()
	if d.character.PK.TotalCount != pkTestTotal-1 {
		t.Fatalf("total %d after entry, want %d", d.character.PK.TotalCount, pkTestTotal-1)
	}
}
