/*
===========================================================================

petbroodcopy_test.go - the brood tick reads nothing outside its narrow copy (#2030)

advanceCrowBrood took the owner's whole record for every crow with a
brood out, per tick. It now takes the companion copy (domain
SnapshotCompanion), as the pet tick does: equipment and companion rows,
no plain bag rows, no avatar inventory. The brood's whole life - the
egg, the hatch, the clone's death storm against a monster - runs twice
on an owner fattened with plain bag rows and an avatar row: once on
the narrow copy and once forced onto the full copy. Every frame must
agree; a reader that reached a plain bag row or the avatar inventory
through its copy would diverge.

===========================================================================
*/
package action

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
broodCopyTrace
================
*/
func broodCopyTrace(t *testing.T, full bool) string {
	t.Helper()
	rt, clock, c, _, key := tailwindFixture(t, 1)
	petPeriodicFatten(c)
	c.AvatarInventory = &enterworld.AvatarInventory{Rows: []enterworld.InventoryRow{{Slot: 0, Codename: "ITEM_AVATAR_TEST"}}}
	if full {
		rt.companionCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
			return rt.characterSnapshot(divisionID, character)
		}
	}
	// Book 51 is the brood row's third option: only BroodBook 3 storms
	// when the clone dies (petbrood.go), so a lower book would leave the
	// death storm's readers unexercised.
	skill := learnPetBook(t, c, 51)
	rt.castCrowBrood(testDivision, c, key.gid, skill, clock.NowMs())
	if len(c.Brood) != 1 {
		t.Fatal("the crow laid no egg; the trace would agree vacuously")
	}
	trace := ""
	step := func(advance time.Duration) {
		clock.now = clock.now.Add(advance)
		trace += fmt.Sprintf("%+v\n", rt.advanceCrowBrood(key, clock.NowMs()))
	}
	step(15 * time.Second)
	step(15 * time.Second)
	if c.Brood[0].BroodStage != 2 {
		t.Fatal("the egg never hatched; the trace would agree vacuously")
	}
	step(time.Second)
	// The clone dies: its death storm strikes the field's monster.
	c.Brood[0].CurrentHP = 0
	step(time.Second)
	if len(c.Brood) != 0 {
		t.Fatal("the dead clone was not removed")
	}
	// The storm's frames ride the monster leg: they join the trace, and
	// exactly one wind visual proves the storm ran on this copy.
	storms := 0
	for _, batch := range rt.drainMonsterLegRecipients() {
		for _, frame := range batch.Frames {
			if frame.Opcode == broodWindOpcode {
				storms++
			}
		}
		trace += fmt.Sprintf("leg %d %+v\n", batch.SourceGID, batch.Frames)
	}
	if storms != 1 {
		t.Fatalf("the clone's death stormed %d times, want 1; the trace would not cover the storm's readers", storms)
	}
	return trace
}

/*
================
TestBroodPassReadsNothingOutsideTheCompanionCopy
================
*/
func TestBroodPassReadsNothingOutsideTheCompanionCopy(t *testing.T) {
	narrow, full := broodCopyTrace(t, false), broodCopyTrace(t, true)
	a, b := strings.Split(narrow, "\n"), strings.Split(full, "\n")
	if len(a) != len(b) {
		t.Fatalf("the traces differ in length: narrow %d, full %d lines", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("line %d diverged\nnarrow: %.900s\nfull:   %.900s", i, a[i], b[i])
		}
	}
}

/*
================
TestBroodTickTakesTheCompanionCopy

The allocation guard: with a brood out and an owner fattened with plain
bag rows, the brood tick on the companion copy must save at least half
of what the whole-record copy costs over the companion copy, measured
against the same tick forced onto the whole-record copy (the
companionCopy seam). Restoring the whole-record copy in the brood tick
(the mutation this guards) saves nothing and fails here.
================
*/
func TestBroodTickTakesTheCompanionCopy(t *testing.T) {
	rt, clock, c, _, key := tailwindFixture(t, 1)
	petPeriodicFatten(c)
	c.AvatarInventory = &enterworld.AvatarInventory{Rows: []enterworld.InventoryRow{{Slot: 0, Codename: "ITEM_AVATAR_TEST"}}}
	skill := learnPetBook(t, c, 49)
	rt.castCrowBrood(testDivision, c, key.gid, skill, clock.NowMs())
	if len(c.Brood) != 1 {
		t.Fatal("the crow laid no egg")
	}
	now := clock.NowMs() + 1000
	bytesPerRun := func(run func()) int64 {
		return testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				run()
			}
		}).AllocedBytesPerOp()
	}
	saving := bytesPerRun(func() { c.Snapshot() }) - bytesPerRun(func() { c.SnapshotCompanion() })
	narrow := bytesPerRun(func() { rt.advanceCrowBrood(key, now) })
	rt.companionCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
		return rt.characterSnapshot(divisionID, character)
	}
	full := bytesPerRun(func() { rt.advanceCrowBrood(key, now) })
	t.Logf("brood tick %d B/run on the companion copy, %d B/run on the whole copy; the copies differ by %d B", narrow, full, saving)
	if full-narrow < saving/2 {
		t.Fatalf("the brood tick saves %d B/run over the whole copy, want at least %d (half the copies' difference)", full-narrow, saving/2)
	}
}
