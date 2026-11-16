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
	"reflect"
	"runtime"
	"strings"
	"testing"

	"opensro.online/server/internal/game/companion"
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
	if state == nil || state.mover.AnyFollower() == nil {
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
	if state.mover.AnyFollower() == nil {
		t.Fatal("copy path lost the follower after the judged abstention")
	}
}

/*
================
TestReadPetTickFactsAllocatesScalarsOnly

The facts a judged tick judges on are eight scalars (petjudged.go's
whole read set); the read must not allocate the companion record
that carries them. Measured with the refusal audit suspended, as
in production: the wide facts container this guard replaced
measured 608 bytes per read on this fixture.
================
*/
func TestReadPetTickFactsAllocatesScalarsOnly(t *testing.T) {
	rt, key, _ := petFollowFixture(t)
	state := rt.petSessionFor(key.division, "benchowner", key.gid)
	if state == nil {
		t.Fatal("pet session missing")
	}
	restore := enterworld.SuspendRefusalAudit(t)
	defer restore()
	read := func() {
		facts := rt.readPetTickFacts(key.division, state.character, key.gid)
		if !facts.admitted {
			t.Fatal("fixture pet not admitted")
		}
	}
	for i := 0; i < 20; i++ {
		read()
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	const calls = 200
	for i := 0; i < calls; i++ {
		read()
	}
	runtime.ReadMemStats(&after)
	perCall := float64(after.TotalAlloc-before.TotalAlloc) / calls
	if perCall > 256 {
		t.Fatalf("readPetTickFacts allocated %.0f bytes per read, want at most 256 (the facts are scalars; the companion record is back)", perCall)
	}
}

/*
================
petWolfFixture

petFollowFixture's bound follow, but the pet is a named band-3
wolf whose owner then moves in the world store as displace says:
a step away in the same region (the chase test) or a warp to a
region the follower cannot walk to (the relocation test). The
wolf has a name and a level so a spawn presentation built from
anything less than the record shows.
================
*/
func petWolfFixture(t *testing.T, displace func(w *simulation.WorldState)) (*Runtime, petOwnerKey, int64) {
	t.Helper()
	c := companionCopyBenchOwner()
	c.ActiveCOS = &enterworld.CharacterCOS{
		GID: 60001, RefObjID: 9, Codename: "COS_P_WOLF_090", Name: "Wolfie", Level: 90,
		CurrentHP: 5000, Summoned: true, Satiety: companion.MaximumSatiety,
	}
	refs := testCosSource(testItems())
	refs.characters["COS_P_WOLF_090"] = &enterworld.CharacterRef{Codename: "COS_P_WOLF_090", RefObjID: 9,
		TidWord: 0x19c6, WalkSpeed: 40, RunSpeed: 80}
	rt, _ := newTestRuntime(c, refs)
	rt.CompanionRoll = func() (uint32, error) { return 0, nil }
	rt.CompanionSurfaceHeight = func(_ uint16, _ float64, y float64, _ float64) (float64, bool) { return y, true }
	rt.ConstrainMovement = func(_ string, from, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) { return to, nil }
	const petGID = uint32(60001)
	rt.BindPetSession(testDivision, c, 101)
	key := petOwnerKey{division: testDivision, name: strings.ToLower(c.Name), gid: petGID}
	now := int64(1000)
	for i := 0; i < 3; i++ {
		rt.advancePet(key, now)
		now += 100
	}
	state := rt.petSessionFor(testDivision, c.Name, petGID)
	if state == nil || state.mover.AnyFollower() == nil {
		t.Fatal("wolf never took up its follow")
	}
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(c) },
		displace)
	return rt, key, now
}

/*
================
petChasingWolfFixture

The wolf whose owner steps away in the world store, far past the
follow distance: the chase installs the follow-run factor on the
live record mid-window, and the judged lane must carry that
installation through its facts or its run speed drifts off the
copy's and the wolf ends somewhere else.
================
*/
func petChasingWolfFixture(t *testing.T) (*Runtime, petOwnerKey, int64) {
	t.Helper()
	return petWolfFixture(t, func(w *simulation.WorldState) { w.Spawn.X += 200 })
}

/*
================
TestAdvancePetJudgedFollowMatchesTheCopyForAChasingWolf

The judged follow and the forced-copy follow of the same wolf,
over the same ten ticks after the owner steps away, must put the
follower at the same spot - and the follower must actually have
run, or the comparison proves nothing. A probe on a third fixture
first proves the judged lane is the one answering.
================
*/
func TestAdvancePetJudgedFollowMatchesTheCopyForAChasingWolf(t *testing.T) {
	probe, probeKey, probeNow := petChasingWolfFixture(t)
	probeState := probe.petSessionFor(probeKey.division, "benchowner", probeKey.gid)
	probeFacts := probe.readPetTickFacts(probeKey.division, probeState.character, probeKey.gid)
	if !probeFacts.admitted {
		t.Fatal("wolf not admitted")
	}
	var following bool
	var answerStuck func() []simulation.Frame
	if _, handled := probe.advancePetJudged(probeKey, probeState, probeFacts, probeNow, &following, &answerStuck); !handled {
		t.Fatal("judged lane abstained on the chasing wolf; the differential would compare copy against copy")
	}

	run := func(forceCopy bool) simulation.Spawn {
		rt, key, now := petChasingWolfFixture(t)
		if forceCopy {
			rt.companionCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
				return character.SnapshotCompanion()
			}
		}
		state := rt.petSessionFor(key.division, "benchowner", key.gid)
		start := state.mover.AnyFollower().Position(now)
		for i := 0; i < 10; i++ {
			now += 100
			rt.advancePet(key, now)
		}
		end := state.mover.AnyFollower().Position(now)
		if end == start {
			t.Fatal("wolf never ran after its owner; the differential is vacuous")
		}
		return end
	}
	judged, copied := run(false), run(true)
	if judged != copied {
		t.Fatalf("judged follow ended at %+v, forced-copy follow at %+v", judged, copied)
	}
}

/*
================
TestAdvancePetJudgedAbstainsWhenTheOwnerWarps

When the owner warps to a region the follower cannot walk to, the
formation step relocates the follower and publishes its respawn
from the pet record (name, body status, life, level). The judged
facts carry none of those, so a judged tick that answered here
would publish a dead, nameless pet: the lane must abstain and let
the copy publish, and the full tick must then match the
forced-copy tick frame for frame.
================
*/
func TestAdvancePetJudgedAbstainsWhenTheOwnerWarps(t *testing.T) {
	warp := func(w *simulation.WorldState) { w.Spawn.RegionID += 2 }

	probe, probeKey, probeNow := petWolfFixture(t, warp)
	probeState := probe.petSessionFor(probeKey.division, "benchowner", probeKey.gid)
	probeFacts := probe.readPetTickFacts(probeKey.division, probeState.character, probeKey.gid)
	if !probeFacts.admitted {
		t.Fatal("wolf not admitted")
	}
	var following bool
	var answerStuck func() []simulation.Frame
	if _, handled := probe.advancePetJudged(probeKey, probeState, probeFacts, probeNow+100, &following, &answerStuck); handled {
		t.Fatal("judged lane answered a relocation tick from its facts view; the respawn would publish a dead, nameless pet")
	}

	run := func(forceCopy bool) []simulation.Frame {
		rt, key, now := petWolfFixture(t, warp)
		if forceCopy {
			rt.companionCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
				return character.SnapshotCompanion()
			}
		}
		frames := rt.advancePet(key, now+100)
		state := rt.petSessionFor(key.division, "benchowner", key.gid)
		if region := state.mover.AnyFollower().Position(now + 100).RegionID; region != 23714 {
			t.Fatalf("follower ended in region %d, want the owner's 23714; the warp never relocated it", region)
		}
		return frames
	}
	judged, copied := run(false), run(true)
	if len(judged) == 0 {
		t.Fatal("the warp tick published nothing; the differential is vacuous")
	}
	if !reflect.DeepEqual(judged, copied) {
		t.Fatalf("judged warp tick published %d frames, forced-copy %d, and they differ", len(judged), len(copied))
	}
}

/*
================
TestReadPetTickFactsIntoScratchAllocatesNothing

advancePet feeds the judged tick from the session's own facts
scratch, so the steady follow tick's fact read allocates nothing at
all: no fresh container per tick and no door closure. The guard
above pins the container's size; this one pins the read itself.
================
*/
func TestReadPetTickFactsIntoScratchAllocatesNothing(t *testing.T) {
	rt, key, _ := petFollowFixture(t)
	state := rt.petSessionFor(key.division, "benchowner", key.gid)
	if state == nil {
		t.Fatal("pet session missing")
	}
	restore := enterworld.SuspendRefusalAudit(t)
	defer restore()
	facts := &petTickFacts{}
	read := func() {
		rt.readPetTickFactsInto(facts, key.division, state.character, key.gid)
	}
	read()
	if !facts.admitted {
		t.Fatal("fixture pet not admitted")
	}
	if allocs := testing.AllocsPerRun(50, read); allocs != 0 {
		t.Fatalf("fact read into scratch allocates %.0f per call, want 0 (a container or a door closure is back)", allocs)
	}
}
