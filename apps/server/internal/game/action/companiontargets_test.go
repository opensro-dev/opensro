/*
===========================================================================

companiontargets_test.go - the monster leg's companion gather, priced and pinned

The leg asks once per owner per leg which summoned companions a
monster may target (Runtime.CompanionTargets, petai.go). The live
profile charges it 1.0 ms a tick at 105 sessions (#3027): every pet
was re-read under its own character door for a catalog radius the
presentation walk had already resolved in hand. The bench below
prices one owner's gather (two summoned attack pets); the test pins
the gather's observable answer - membership, order, radius, pose,
and the native exclusions - so the door change underneath it cannot
move behaviour.

===========================================================================
*/

package action

import (
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

var companionTargetsBenchSink []simulation.CompanionTarget

/*
================
companionTargetsFixture

One owner with two summoned attack pets: the active COS and a second
record riding a mission-inventory summoner row, each with a bound
follower at the owner's spawn. Radii differ per pet (10 and 14) so a
target's radius proves which catalog row it came from.
================
*/
func companionTargetsFixture(tb testing.TB) (*Runtime, *fakeClock, *enterworld.Character, uint32, uint32) {
	tb.Helper()
	rt, clock, c, _ := newCombatTestRuntime(tb, 100)
	ref1 := &enterworld.CharacterRef{RefObjID: 9, Codename: "PET_ATK", TidWord: 3<<11 | 0x1c6,
		Level: 1, MaxHP: 100000, MaxMP: 600, WalkSpeed: 20, RunSpeed: 80,
		Parameters: monster.MonsterRef{CombatPinned: true, BodyRadius: 10, HitRate: 10}}
	ref2 := &enterworld.CharacterRef{RefObjID: 9, Codename: "PET_ATK2", TidWord: 3<<11 | 0x1c6,
		Level: 1, MaxHP: 100000, MaxMP: 600, WalkSpeed: 20, RunSpeed: 80,
		Parameters: monster.MonsterRef{CombatPinned: true, BodyRadius: 14, HitRate: 10}}
	grab := &enterworld.CharacterRef{RefObjID: 9, Codename: "PET_GRAB", TidWord: 4<<11 | 0x1c6,
		Level: 1, MaxHP: 100000, MaxMP: 600, WalkSpeed: 20, RunSpeed: 80,
		Parameters: monster.MonsterRef{CombatPinned: true, BodyRadius: 99, HitRate: 10}}
	items := cosTestItemSource{staticItemSource: testItems(), characters: map[string]*enterworld.CharacterRef{
		ref1.Codename: ref1, ref2.Codename: ref2, grab.Codename: grab,
	}}
	rt.deps.(*enterworld.Deps).Items = items
	gid1, valid := enterworld.CosObjectIDForCharacter(c)
	if !valid {
		tb.Fatal("fixture has no COS identity")
	}
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid1, RefObjID: ref1.RefObjID, Codename: ref1.Codename,
		Level: 1, CurrentHP: ref1.MaxHP, CurrentMP: ref1.MaxMP, StateFlags: 1, Summoned: true}
	const gid2 = uint32(60002)
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
		Slot: 21, RefObjID: 62, Codename: "ITEM_COS_PET",
		Summon: &enterworld.CharacterCOS{GID: gid2, RefObjID: ref2.RefObjID, Codename: ref2.Codename,
			Level: 1, CurrentHP: ref2.MaxHP, CurrentMP: ref2.MaxMP, StateFlags: 1, Summoned: true},
	})
	rt.Now = clock.Now
	rt.BindPetSession(testDivision, c, 1)
	spawn := rt.LiveSpawnFor(testDivision, c)
	for _, gid := range []uint32{gid1, gid2} {
		state := rt.petSessionFor(testDivision, c.Name, gid)
		if state == nil {
			rt.petMu.Lock()
			if rt.petSessions == nil {
				rt.petSessions = make(map[petOwnerKey]*petSession)
			}
			state = &petSession{character: c, ready: true}
			rt.petSessions[petOwnerKey{division: testDivision, name: strings.ToLower(c.Name), gid: gid}] = state
			rt.petMu.Unlock()
		}
		state.mover.FollowFrom(gid, spawn)
		state.refObjID = 9
	}
	return rt, clock, c, gid1, gid2
}

/*
================
TestCompanionTargetsGathersSummonedPetsInOrder

Both summoned pets are offered in companion order, each with its own
catalog radius and its follower's pose; the native exclusions (dead,
mounted, pickup band, a codename the catalog no longer knows) each
remove exactly their own pet.
================
*/
func TestCompanionTargetsGathersSummonedPetsInOrder(t *testing.T) {
	rt, clock, c, gid1, gid2 := companionTargetsFixture(t)
	ownerGID := enterworld.ObjectIDForCharacter(c)
	targets := rt.CompanionTargets(testDivision, ownerGID, clock.NowMs())
	if len(targets) != 2 {
		t.Fatalf("targets = %+v, want both summoned pets", targets)
	}
	if targets[0].Gid != gid1 || targets[1].Gid != gid2 {
		t.Fatalf("target order = %d, %d; want %d, %d", targets[0].Gid, targets[1].Gid, gid1, gid2)
	}
	if targets[0].BodyRadius != 10 || targets[1].BodyRadius != 14 {
		t.Fatalf("radii = %v, %v; want 10, 14", targets[0].BodyRadius, targets[1].BodyRadius)
	}
	spawn := rt.LiveSpawnFor(testDivision, c)
	for _, target := range targets {
		if target.Band != 3 || target.Pose.RegionID != spawn.RegionID || target.Pose.X != spawn.X || target.Pose.Z != spawn.Z {
			t.Fatalf("target = %+v, want band 3 at the owner spawn %+v", target, spawn)
		}
	}

	// Each native exclusion removes exactly its own pet.
	exclusions := []struct {
		name  string
		apply func(c *enterworld.Character)
		want  uint32
	}{
		{"dead active pet", func(c *enterworld.Character) { c.ActiveCOS.CurrentHP = 0 }, gid2},
		{"mounted active pet", func(c *enterworld.Character) { c.ActiveCOS.Mounted = true }, gid2},
		{"pickup second pet", func(c *enterworld.Character) {
			c.MissionInventory[len(c.MissionInventory)-1].Summon.Codename = "PET_GRAB"
		}, gid1},
		{"unknown second pet", func(c *enterworld.Character) {
			c.MissionInventory[len(c.MissionInventory)-1].Summon.Codename = "PET_GONE"
		}, gid1},
	}
	for _, exclusion := range exclusions {
		rt, clock, c, _, _ := companionTargetsFixture(t)
		exclusion.apply(c)
		targets := rt.CompanionTargets(testDivision, enterworld.ObjectIDForCharacter(c), clock.NowMs())
		if len(targets) != 1 || targets[0].Gid != exclusion.want {
			t.Fatalf("%s: targets = %+v, want only gid %d", exclusion.name, targets, exclusion.want)
		}
	}
}

/*
================
BenchmarkCompanionTargetsLegGather

One owner's leg gather: find the owner, present its two summoned
pets, and resolve each pet's target row. The leg pays this per
owner per leg; the profile names it under RunMonsterLeg (#3027).
================
*/
func BenchmarkCompanionTargetsLegGather(b *testing.B) {
	rt, clock, c, _, _ := companionTargetsFixture(b)
	ownerGID := enterworld.ObjectIDForCharacter(c)
	if targets := rt.CompanionTargets(testDivision, ownerGID, clock.NowMs()); len(targets) != 2 {
		b.Fatalf("gather offered %d targets, want 2", len(targets))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		companionTargetsBenchSink = rt.CompanionTargets(testDivision, ownerGID, clock.NowMs())
	}
}
