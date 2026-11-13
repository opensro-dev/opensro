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
	"fmt"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/data/store"
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
TestTickSnapshotCloneBudget

The clones are the cost the cache removes (#3099): a root served
from the tick's cache allocates nothing, and the same root outside
the bracket pays the clone - so the budget breaks if sharing
silently stops working in either direction.
================
*/
func TestTickSnapshotCloneBudget(t *testing.T) {
	d := tickSnapshotDoorRuntime(t)
	rt, c := d.rt, d.character

	rt.beginTickSnapshots()
	if got := rt.characterSnapshotCast(testDivision, c); got == nil {
		t.Fatal("the warm-up root returned nothing")
	}
	if allocs := testing.AllocsPerRun(50, func() {
		_ = rt.characterSnapshotCast(testDivision, c)
	}); allocs != 0 {
		t.Fatalf("a shared cast root allocated %v per run, want 0", allocs)
	}
	rt.endTickSnapshots()

	if allocs := testing.AllocsPerRun(10, func() {
		_ = rt.characterSnapshotCast(testDivision, c)
	}); allocs == 0 {
		t.Fatal("an unshared cast root allocated nothing: the clone is not being measured")
	}
}

// tickSnapshotBenchPlayers is the synthetic population the benchmark
// ticks over: 100 characters with a carried bag, on a real store.
const tickSnapshotBenchPlayers = 100

/*
================
tickSnapshotBenchRuntime
================
*/
func tickSnapshotBenchRuntime(b *testing.B) (*Runtime, []*enterworld.Character) {
	b.Helper()
	authority, err := store.Open(filepath.Join(b.TempDir(), "authority"), store.Options{DefaultSkills: doorSkillSeeder})
	if err != nil {
		b.Fatalf("store.Open: %v", err)
	}
	b.Cleanup(authority.Close)
	deps := &enterworld.Deps{
		Characters: authority.Characters(),
		Items:      testItems(),
	}
	deps.ReadCharacter = func(_ string, read func()) { authority.ReadState(read) }
	deps.ReadLock = func(string) { authority.ReadLock() }
	deps.ReadUnlock = func(string) { authority.ReadUnlock() }
	deps.CharacterWriteEpoch = authority.CharacterWriteEpoch
	rt := NewRuntime(deps, nil)

	characters := make([]*enterworld.Character, 0, tickSnapshotBenchPlayers)
	for i := range tickSnapshotBenchPlayers {
		seed := testCharacter()
		seed.Name = fmt.Sprintf("Bench%03d", i)
		for slot := 0; slot < 24; slot++ {
			row := seed.MissionInventory[0]
			row.Slot = int64(30 + slot)
			seed.MissionInventory = append(seed.MissionInventory, row)
		}
		if err := authority.CreateCharacter(testDivision, fmt.Sprintf("bench-%03d", i), seed); err != nil {
			b.Fatalf("CreateCharacter: %v", err)
		}
		characters = append(characters, seed)
	}
	return rt, characters
}

/*
================
BenchmarkTickSnapshotRoots100

One tick's snapshot-root traffic over 100 synthetic players: the
press, finalize, pet and presentation roots ask for the same
character's cast, combat and companion copies several times a tick
(the 2026-11-10 profile put the repeated read-door clones at about
10 ms of the 61 ms tick). The pair pins the store-clone share of the
action hook with sharing on and off; off is the pre-#3099 behaviour,
where every root cloned. Run with -benchmem: clones are allocations.
================
*/
func BenchmarkTickSnapshotRoots100(b *testing.B) {
	rt, characters := tickSnapshotBenchRuntime(b)
	roots := func() {
		for _, c := range characters {
			_ = rt.characterSnapshotCast(testDivision, c)
			_ = rt.characterSnapshotCast(testDivision, c)
			_ = rt.characterSnapshotCast(testDivision, c)
			_ = rt.characterSnapshotCombat(testDivision, c)
			_ = rt.characterSnapshotCombat(testDivision, c)
			_ = rt.characterSnapshotCombatAdmitted(testDivision, c, nil)
			_ = rt.characterSnapshotCompanionAdmitted(testDivision, c, nil)
			_ = rt.characterSnapshotCompanionAdmitted(testDivision, c, nil)
			_ = rt.characterSnapshot(testDivision, c)
		}
	}
	b.Run("shared", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			rt.beginTickSnapshots()
			roots()
			rt.endTickSnapshots()
		}
	})
	b.Run("unshared", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			roots()
		}
	})
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

/*
================
TestKeptCastCopyOutlivesTheTick

EntryActionSpeed's cast copy (#3427) is read in prepareTick, outside
any bracket: it is kept across ticks while the write epoch holds,
retired by a door write, released once no read touches it for a full
tick, and costs no allocation when served.
================
*/
func TestKeptCastCopyOutlivesTheTick(t *testing.T) {
	d := tickSnapshotDoorRuntime(t)
	rt, c := d.rt, d.character

	first := rt.keptCastCopy(testDivision, c)
	rt.rotateKeptCasts()
	second := rt.keptCastCopy(testDivision, c)
	if first == nil || first != second {
		t.Fatal("an unchanged record was cloned again across a tick")
	}
	if allocs := testing.AllocsPerRun(50, func() {
		_ = rt.keptCastCopy(testDivision, c)
	}); allocs != 0 {
		t.Fatalf("a kept cast copy allocated %v per read, want 0", allocs)
	}

	if !rt.deps.Update(c, "kept-cast-test", func() bool {
		c.DeletePending = true
		return true
	}) {
		t.Fatal("the writing door refused")
	}
	written := rt.keptCastCopy(testDivision, c)
	if written == second || written == nil || !written.DeletePending {
		t.Fatal("a door write did not retire the kept copy")
	}

	rt.rotateKeptCasts()
	rt.rotateKeptCasts()
	if again := rt.keptCastCopy(testDivision, c); again == written {
		t.Fatal("a copy no read touched for a full tick was still kept")
	}
}

/*
================
TestKeptCastCopyUnwiredClones
================
*/
func TestKeptCastCopyUnwiredClones(t *testing.T) {
	d := openDoorRuntime(t, filepath.Join(t.TempDir(), "authority"), testCharacter())
	rt, c := d.rt, d.character
	if a, b := rt.keptCastCopy(testDivision, c), rt.keptCastCopy(testDivision, c); a == nil || a == b {
		t.Fatal("without the write epoch wired the cast copy was shared")
	}
}

/*
================
BenchmarkEntryActionSpeed100

prepareTick's per-session EntryActionSpeed over 100 synthetic players
(#3427): kept is a steady-state tick, where the cast copies outlive
the tick; cloned is the pre-#3427 behaviour, where every read cloned.
================
*/
func BenchmarkEntryActionSpeed100(b *testing.B) {
	rt, characters := tickSnapshotBenchRuntime(b)
	b.Run("kept", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			for _, c := range characters {
				_ = rt.EntryActionSpeed(testDivision, c.Name)
			}
			rt.rotateKeptCasts()
		}
	})
	b.Run("cloned", func(b *testing.B) {
		// The castCopy seam bypasses the kept copies: every read clones,
		// through the same door the cast root uses.
		rt.castCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
			var snapshot *enterworld.Character
			rt.deps.Read(divisionID, func() { snapshot = character.SnapshotCast() })
			return snapshot
		}
		defer func() { rt.castCopy = nil }()
		b.ReportAllocs()
		for range b.N {
			for _, c := range characters {
				_ = rt.EntryActionSpeed(testDivision, c.Name)
			}
		}
	})
}
