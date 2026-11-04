/*
===========================================================================

petjudged_test.go - the bound follow tick pays no owner copy

The pet tick used to take the narrowed owner copy on every admitted
tick (SnapshotCompanion: measured 201 allocations and 26,437 bytes
per tick on the settled-owner fixture). A bound, following pet with
no battle, roar or pickup live now judges on facts read under one
door (readPetTickFacts) and takes the copy only when the tick must
commit through the record (#2139). This suite holds that line.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

// petFollowAllocBound caps advancePet's steady follow tick: the
// judged tick measures 13 allocations; the forced-copy control on the
// same fixture measures 201. The bound sits between them, so the
// copy coming back - through readPetTickFacts growing a clone, or
// the judged path abstaining - fails here.
const petFollowAllocBound = 26

/*
================
TestAdvancePetFollowTickDoesNotCopyTheOwner

One admitted follow tick per run on the settled-owner fixture, warm
first, must stay under the bound; the same tick with the companion
copy forced through the test seam must blow it, which proves the
bound bites. The follower must still be bound afterwards: the judged
tick followed, it did not fall out.
================
*/
func TestAdvancePetFollowTickDoesNotCopyTheOwner(t *testing.T) {
	rt, key, now := petFollowFixture(t)
	tick := func() {
		now += 100
		rt.advancePet(key, now)
	}
	judged := testing.AllocsPerRun(50, tick)
	if judged > petFollowAllocBound {
		t.Fatalf("follow tick allocates %.0f per run, want at most %d (the judged tick measures 13; the owner copy is back)",
			judged, petFollowAllocBound)
	}

	rt.companionCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
		return character.SnapshotCompanion()
	}
	forced := testing.AllocsPerRun(50, tick)
	if forced <= petFollowAllocBound {
		t.Fatalf("forced-copy tick allocates %.0f per run, want over %d (the bound cannot see the copy)",
			forced, petFollowAllocBound)
	}

	state := rt.petSessionFor(key.division, "benchowner", key.gid)
	if state == nil || state.follower == nil {
		t.Fatal("pet lost its follower over the measured ticks")
	}
}

/*
================
TestAdvancePetJudgedAbstainsWithoutAnOwnerWorld

The judged tick must never seed the owner world from the live
record: seeding reads the record outside the character door. With
the store emptied, advancePetJudged abstains and the copy path
(which seeds from its door-safe copy) still completes the tick;
with the entry present the same call is handled.
================
*/
func TestAdvancePetJudgedAbstainsWithoutAnOwnerWorld(t *testing.T) {
	rt, key, now := petFollowFixture(t)
	state := rt.petSessionFor(key.division, "benchowner", key.gid)
	if state == nil {
		t.Fatal("pet session missing")
	}
	judge := func() bool {
		facts := rt.readPetTickFacts(key.division, state.character, key.gid)
		if !facts.admitted {
			t.Fatal("fixture pet not admitted")
		}
		var following bool
		var answerStuck func() []simulation.Frame
		_, handled := rt.advancePetJudged(key, state, facts, now, &following, &answerStuck)
		return handled
	}
	if !judge() {
		t.Fatal("judged tick abstained with the owner world present")
	}
	rt.Worlds = simulation.NewWorldStore()
	if judge() {
		t.Fatal("judged tick seeded the owner world from the live record")
	}
	rt.advancePet(key, now+100)
	if state.follower == nil {
		t.Fatal("copy path lost the follower after the judged abstention")
	}
}
