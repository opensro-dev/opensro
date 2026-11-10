/*
===========================================================================

tick_snapshots_test.go - the tick's shared record copies (#3099)

Inside one tick bracket the snapshot roots serve one copy per
character and copy kind; a door write retires it; outside the bracket
- or without the write epoch wired - every root clones as before.

===========================================================================
*/

package action

import (
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
tickSnapshotDoorRuntime

openDoorRuntime with the read door and the write epoch wired from
the real store, as the production composition wires them.
================
*/
func tickSnapshotDoorRuntime(t *testing.T) *doorRuntime {
	t.Helper()
	d := openDoorRuntime(t, filepath.Join(t.TempDir(), "authority"), testCharacter())
	deps := d.rt.deps.(*enterworld.Deps)
	deps.ReadCharacter = func(_ string, read func()) { d.authority.ReadState(read) }
	deps.ReadLock = func(string) { d.authority.ReadLock() }
	deps.ReadUnlock = func(string) { d.authority.ReadUnlock() }
	deps.CharacterWriteEpoch = d.authority.CharacterWriteEpoch
	return d
}

/*
================
TestTickSnapshotsShareWithinTheBracket
================
*/
func TestTickSnapshotsShareWithinTheBracket(t *testing.T) {
	d := tickSnapshotDoorRuntime(t)
	rt, c := d.rt, d.character

	rt.beginTickSnapshots()
	cast1 := rt.characterSnapshotCast(testDivision, c)
	cast2 := rt.characterSnapshotCast(testDivision, c)
	combat := rt.characterSnapshotCombat(testDivision, c)
	rt.endTickSnapshots()

	if cast1 == nil || cast1 != cast2 {
		t.Fatal("two cast roots in one tick did not share their copy")
	}
	if combat == nil || combat == cast1 {
		t.Fatal("the combat root shared the cast copy's shape")
	}
	if fresh := rt.characterSnapshotCast(testDivision, c); fresh == cast1 {
		t.Fatal("a root outside the bracket served the tick's copy")
	}
}

/*
================
TestTickSnapshotsRetireOnWrite
================
*/
func TestTickSnapshotsRetireOnWrite(t *testing.T) {
	d := tickSnapshotDoorRuntime(t)
	rt, c := d.rt, d.character

	rt.beginTickSnapshots()
	before := rt.characterSnapshotCast(testDivision, c)
	if before == nil || before.DeletePending {
		t.Fatal("the seeded record reads as deletion-pending")
	}
	if !rt.deps.Update(c, "tick-snapshot-test", func() bool {
		c.DeletePending = true
		return true
	}) {
		t.Fatal("the writing door refused")
	}
	after := rt.characterSnapshotCast(testDivision, c)
	rt.endTickSnapshots()

	if after == before {
		t.Fatal("the root served the copy a door write had retired")
	}
	if after == nil || !after.DeletePending {
		t.Fatal("the fresh copy does not carry the tick's own write")
	}
}

/*
================
TestTickSnapshotsAdmissionJudgesTheLiveRecord
================
*/
func TestTickSnapshotsAdmissionJudgesTheLiveRecord(t *testing.T) {
	d := tickSnapshotDoorRuntime(t)
	rt, c := d.rt, d.character
	alive := func(character *enterworld.Character) bool { return !character.DeletePending }

	rt.beginTickSnapshots()
	if got := rt.characterSnapshotCombatAdmitted(testDivision, c, alive); got == nil {
		t.Fatal("a live record was refused")
	}
	if !rt.deps.Update(c, "tick-snapshot-test", func() bool {
		c.DeletePending = true
		return true
	}) {
		t.Fatal("the writing door refused")
	}
	if got := rt.characterSnapshotCombatAdmitted(testDivision, c, alive); got != nil {
		t.Fatal("the admitted root served a copy of a record admission now refuses")
	}
	rt.endTickSnapshots()
}

/*
================
TestTickSnapshotsNeedTheEpoch
================
*/
func TestTickSnapshotsNeedTheEpoch(t *testing.T) {
	d := openDoorRuntime(t, filepath.Join(t.TempDir(), "authority"), testCharacter())
	deps := d.rt.deps.(*enterworld.Deps)
	deps.ReadCharacter = func(_ string, read func()) { d.authority.ReadState(read) }
	rt, c := d.rt, d.character

	rt.beginTickSnapshots()
	first := rt.characterSnapshotCast(testDivision, c)
	second := rt.characterSnapshotCast(testDivision, c)
	rt.endTickSnapshots()

	if first == nil || second == nil || first == second {
		t.Fatal("copies were shared without the write epoch wired")
	}
}
