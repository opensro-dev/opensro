/*
===========================================================================

petspirit_facts_test.go - the spirit expiry judged on door-read values (#2030)

advancePetSpirit copied the pet owner on every tick a bear held
Resolve stacks, only to decide whether the stacks still stand. The
verdict now answers from door-read facts; only an expiry the facts
cannot judge takes the copy. Each case runs the whole advance twice
on one runtime: once with the facts live, and once behind the
forced-copy seam, which makes the facts abstain and the copy judge
as before. The emitted frames and the surviving stack count must
agree, and the standing tick must stop paying for the copy.

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestPetSpiritFactsAgreeWithTheCopy
================
*/
func TestPetSpiritFactsAgreeWithTheCopy(t *testing.T) {
	verdict := func(t *testing.T, rt *Runtime, key petOwnerKey, c *enterworld.Character, now int64) ([]simulation.Frame, uint8) {
		t.Helper()
		state := rt.petSessionFor(key.division, key.name, key.gid)
		if state == nil {
			t.Fatal("spirit fixture has no pet session")
		}
		petBefore := *c.ActiveCOS
		trainedBefore := state.trained
		factsFrames := rt.advancePetSpirit(key, now)
		factsStacks := c.ActiveCOS.ResolveStacks
		*c.ActiveCOS = petBefore
		state.trained = trainedBefore
		rt.companionCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
			return character.SnapshotCompanion()
		}
		copyFrames := rt.advancePetSpirit(key, now)
		rt.companionCopy = nil
		copyStacks := c.ActiveCOS.ResolveStacks
		if factsStacks != copyStacks || len(factsFrames) != len(copyFrames) {
			t.Fatalf("verdict split: facts stacks=%d frames=%d, copy stacks=%d frames=%d",
				factsStacks, len(factsFrames), copyStacks, len(copyFrames))
		}
		for i := range factsFrames {
			if factsFrames[i].Opcode != copyFrames[i].Opcode || !bytes.Equal(factsFrames[i].Payload, copyFrames[i].Payload) {
				t.Fatalf("frame split at %d: facts opcode=%#x payload=%x, copy opcode=%#x payload=%x",
					i, factsFrames[i].Opcode, factsFrames[i].Payload, copyFrames[i].Opcode, copyFrames[i].Payload)
			}
		}
		return factsFrames, factsStacks
	}
	bear := func(t *testing.T) (*Runtime, *fakeClock, *enterworld.Character, petOwnerKey) {
		t.Helper()
		rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
		makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
		rt.AttackPetExpansion = true
		roarField(t, rt, c, clock)
		learnPetBook(t, c, 22)
		pet := c.ActiveCOS
		for range 3 {
			rt.gainPetSpirit(testDivision, c, pet, clock.NowMs())
		}
		if pet.ResolveStacks != 3 {
			t.Fatalf("spirit fixture holds %d stacks, want 3", pet.ResolveStacks)
		}
		return rt, clock, c, petOwnerKey{division: testDivision, name: c.Name, gid: pet.GID}
	}

	// Standing stacks inside their window: no frames, no expiry, and
	// the tick no longer pays for the pet-owner copy.
	t.Run("standing agrees", func(t *testing.T) {
		rt, clock, c, key := bear(t)
		frames, stacks := verdict(t, rt, key, c, clock.NowMs())
		if stacks != 3 || frames != nil {
			t.Fatalf("standing stacks expired: stacks=%d frames=%v", stacks, frames)
		}
		copyAllocs := testing.AllocsPerRun(20, func() {
			rt.characterSnapshotCompanionAdmitted(testDivision, c, func(*enterworld.Character) bool { return true })
		})
		standingAllocs := testing.AllocsPerRun(20, func() { rt.advancePetSpirit(key, clock.NowMs()) })
		t.Logf("allocs: one companion copy %.0f, standing spirit tick %.0f", copyAllocs, standingAllocs)
		if standingAllocs >= copyAllocs {
			t.Fatalf("standing spirit tick still copies the owner: %.0f allocs, one copy is %.0f", standingAllocs, copyAllocs)
		}
	})

	// The window closed: both paths expire through the same door.
	t.Run("expired window agrees", func(t *testing.T) {
		rt, clock, c, key := bear(t)
		frames, stacks := verdict(t, rt, key, c, clock.NowMs()+petSpiritLifetime)
		if stacks != 0 || len(frames) == 0 {
			t.Fatalf("expired stacks stood: stacks=%d frames=%d", stacks, len(frames))
		}
	})

	// An unsummoned bear is invisible to the lane: CompanionByGID
	// walks summoned rows only, so both paths leave the stacks for
	// the resummon tick to judge.
	t.Run("unsummoned agrees", func(t *testing.T) {
		rt, clock, c, key := bear(t)
		c.ActiveCOS.Summoned = false
		frames, stacks := verdict(t, rt, key, c, clock.NowMs())
		if stacks != 3 || frames != nil {
			t.Fatalf("unsummoned bear judged: stacks=%d frames=%v", stacks, frames)
		}
	})

	// A dead bear cannot retain the heal.
	t.Run("dead pet agrees", func(t *testing.T) {
		rt, clock, c, key := bear(t)
		c.ActiveCOS.CurrentHP = 0
		if _, stacks := verdict(t, rt, key, c, clock.NowMs()); stacks != 0 {
			t.Fatalf("dead bear kept its stacks: %d", stacks)
		}
	})

	// A dead owner cannot retain the heal.
	t.Run("dead owner agrees", func(t *testing.T) {
		rt, clock, c, key := bear(t)
		dead := int64(0)
		c.CurrentHP = &dead
		if _, stacks := verdict(t, rt, key, c, clock.NowMs()); stacks != 0 {
			t.Fatalf("dead owner's bear kept its stacks: %d", stacks)
		}
	})

	// A different book in the slot than the session earned with.
	t.Run("book mismatch agrees", func(t *testing.T) {
		rt, clock, c, key := bear(t)
		state := rt.petSessionFor(key.division, key.name, key.gid)
		state.trained.resolveBook++
		if _, stacks := verdict(t, rt, key, c, clock.NowMs()); stacks != 0 {
			t.Fatalf("mismatched book kept its stacks: %d", stacks)
		}
	})

	// The expansion's owner half refuses off the default world
	// instance: the stacks fall.
	t.Run("world instance agrees", func(t *testing.T) {
		rt, clock, c, key := bear(t)
		instance := domain.DefaultWorldInstance + 1
		c.World.PackedInstance = &instance
		if _, stacks := verdict(t, rt, key, c, clock.NowMs()); stacks != 0 {
			t.Fatalf("off-instance bear kept its stacks: %d", stacks)
		}
	})

	// No stacks: neither path has anything to judge or expire.
	t.Run("no stacks agrees", func(t *testing.T) {
		rt, clock, c, key := bear(t)
		c.ActiveCOS.ResolveStacks = 0
		frames, stacks := verdict(t, rt, key, c, clock.NowMs())
		if stacks != 0 || frames != nil {
			t.Fatalf("stackless bear gained frames: stacks=%d frames=%v", stacks, frames)
		}
	})

	// In town the heal cannot stand: the town verdict comes from the
	// owner world entry, as the copy's inTown reads it.
	t.Run("town agrees", func(t *testing.T) {
		rt, clock, c, key := bear(t)
		town := uint16(0)
		for id := 0; id <= 0xffff; id++ {
			if townRegion(uint16(id)) {
				town = uint16(id)
				break
			}
		}
		if town == 0 {
			t.Fatal("no town region in the test data")
		}
		rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name),
			func() simulation.WorldState { return simulation.SeedWorldState(c) },
			func(w *simulation.WorldState) { w.Spawn.RegionID = town })
		if _, stacks := verdict(t, rt, key, c, clock.NowMs()); stacks != 0 {
			t.Fatalf("town bear kept its stacks: %d", stacks)
		}
	})

	// Without a world entry the facts abstain and the copy judges, as
	// before: the field verdict stands either way.
	t.Run("no world entry abstains", func(t *testing.T) {
		rt, clock, c, key := bear(t)
		rt.Worlds.Forget(simulation.WorldKey(testDivision, c.Name))
		if _, ok := rt.Worlds.SnapshotExisting(simulation.WorldKey(testDivision, c.Name)); ok {
			t.Fatal("world entry survived the forget")
		}
		frames, stacks := verdict(t, rt, key, c, clock.NowMs())
		if stacks != 3 || frames != nil {
			t.Fatalf("abstained tick changed the verdict: stacks=%d frames=%v", stacks, frames)
		}
	})
}
