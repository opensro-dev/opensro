/*
===========================================================================

petcopy_test.go - the pet tick reads nothing outside its narrow copy (#2030)

advancePet took the owner's whole record per summoned pet per tick.
It now takes the companion copy (domain SnapshotCompanion): equipment
and companion rows, no plain bag rows, no avatar inventory. The
scenario here runs twice on a bag loaded with plain rows - once on
the narrow copy and once forced onto the full copy - and every frame
and the follower state must agree. A reader that reached a plain bag
row or an avatar row through its copy would diverge or lose it.

===========================================================================
*/
package action

import (
	"fmt"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
petCopyTrace

A bag-row pet takes up its follow over three ticks, the owner fattened
with plain bag rows the narrow copy drops.
================
*/
func petCopyTrace(t *testing.T, full bool) string {
	c := testCharacter()
	c.ID, c.Name = 77, "bagowner"
	refs := testCosSource(testItems())
	refs.characters["PET"] = &enterworld.CharacterRef{Codename: "PET", RefObjID: 9, TidWord: 0x21c6, RunSpeed: 80}
	rt, _ := newTestRuntime(c, refs)
	rt.CompanionRoll = func() (uint32, error) { return 0, nil }
	rt.CompanionSurfaceHeight = func(_ uint16, _ float64, y float64, _ float64) (float64, bool) { return y, true }
	rt.ConstrainMovement = func(_ string, from, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) { return to, nil }
	const petGID = uint32(60001)
	pet := &enterworld.CharacterCOS{GID: petGID, RefObjID: 9, Codename: "PET", CurrentHP: 100, Summoned: true}
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 13, Summon: pet})
	c.AvatarInventory = &enterworld.AvatarInventory{Rows: []enterworld.InventoryRow{{Slot: 0, Codename: "ITEM_AVATAR_TEST"}}}
	petPeriodicFatten(c)
	if full {
		rt.companionCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
			return rt.characterSnapshot(divisionID, character)
		}
	}
	rt.BindPetSession(testDivision, c, 101)
	key := petOwnerKey{division: testDivision, name: strings.ToLower(c.Name), gid: petGID}

	trace := ""
	for _, nowMs := range []int64{1000, 1100, 1300} {
		trace += fmt.Sprintf("%+v\n", rt.advancePet(key, nowMs))
	}
	state := rt.petSessionFor(testDivision, c.Name, petGID)
	if state == nil || state.follower == nil {
		t.Fatal("pet never took up its follow; the trace would agree vacuously")
	}
	return trace + fmt.Sprintf("follower gid=%d pos=%+v", state.follower.GID(), state.follower.Position(1300))
}

/*
================
TestPetPassReadsNothingOutsideTheCompanionCopy
================
*/
func TestPetPassReadsNothingOutsideTheCompanionCopy(t *testing.T) {
	narrow, full := petCopyTrace(t, false), petCopyTrace(t, true)
	a, b := strings.Split(narrow, "\n"), strings.Split(full, "\n")
	for i := range a {
		if i >= len(b) || a[i] != b[i] {
			t.Fatalf("line %d diverged\nnarrow: %.900s\nfull:   %.900s", i, a[i], b[min(i, len(b)-1)])
		}
	}
}

/*
================
TestCompanionCopyCostsLessThanHalfTheFullCopy

The allocation guard on the bench-shaped owner: a full bag, a
summoned pet. Restoring the whole-record copy in the pet pass (the
mutation this guards) at least doubles the detached bytes.
================
*/
func TestCompanionCopyCostsLessThanHalfTheFullCopy(t *testing.T) {
	c := petTickBenchOwner(1)
	bytesPerRun := func(copy func() *enterworld.Character) int64 {
		result := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if copy() == nil {
					b.Fatal("no copy")
				}
			}
		})
		return result.AllocedBytesPerOp()
	}
	full := bytesPerRun(c.Snapshot)
	narrow := bytesPerRun(c.SnapshotCompanion)
	t.Logf("owner copy %d B/run, companion copy %d B/run; rows full=%d narrow=%d", full, narrow,
		len(c.Snapshot().MissionInventory), len(c.SnapshotCompanion().MissionInventory))
	if narrow >= full/2 {
		t.Fatalf("companion copy allocates %d B/run, want under half the %d B full copy", narrow, full)
	}
}

/*
================
TestPetAreaVictimsReadNothingOutsideTheCompanionCopy

The area selector copies every present player to run its refusal
chains. A fattened victim (stuffed bag, avatar rows, slot-8 cape)
must be selected under a different cape and refused under the same
cape identically on the companion copy and on the whole-record copy,
and the seam must see the victim's copy.
================
*/
func TestPetAreaVictimsReadNothingOutsideTheCompanionCopy(t *testing.T) {
	rt, clock, owner, victim := newPvpPair(t)
	ref := equipCombatTestPet(t, rt, owner, attackPetBand)
	rt.BindPetSession(testDivision, owner, 1)
	pet := *owner.ActiveCOS
	var valid bool
	pet.GID, valid = enterworld.CosObjectIDForCharacter(victim)
	if !valid {
		t.Fatal("victim has no companion identity")
	}
	victim.ActiveCOS = &pet
	rt.BindPetSession(testDivision, victim, 1)
	rt.RewardActorPresent = func(string, string) bool { return true }
	owner.Level, victim.Level = testInt64(playerCombatMinimumLevel), testInt64(playerCombatMinimumLevel)
	victim.Aggressions = map[uint32]uint32{enterworld.ObjectIDForCharacter(owner): playerAggressionTicks}
	items := rt.deps.ItemReferences().(cosTestItemSource).staticItemSource
	capes := map[int]*enterworld.ItemRef{}
	for color := 1; color <= 2; color++ {
		cape := &enterworld.ItemRef{
			RefObjID: uint32(63000 + color), Codename: fmt.Sprintf("AREA_CAPE_%d", color),
			TypeIDs:      [4]int64{3, 1, 7, 5},
			NativeFields: enterworld.NewNativeFields(map[string]float64{freeBattleGroupField: float64(color)}),
		}
		items[cape.Codename], capes[color] = cape, cape
	}
	setCape := func(actor *enterworld.Character, color int) {
		var inventory []enterworld.InventoryRow
		for _, row := range actor.MissionInventory {
			if row.Slot != int64(jobSuitSlot) {
				inventory = append(inventory, row)
			}
		}
		if cape := capes[color]; cape != nil {
			inventory = append(inventory, enterworld.InventoryRow{
				Slot: int64(jobSuitSlot), RefObjID: cape.RefObjID, Codename: cape.Codename, TypeFlags: cape.TypeFlags(), StackCount: 1,
			})
		}
		actor.MissionInventory = inventory
	}
	stuffOwnerInventories(victim)
	state := rt.petSessionFor(testDivision, owner.Name, owner.ActiveCOS.GID)
	at := rt.liveSpawn(simulation.WorldKey(testDivision, owner.Name), owner, clock.NowMs())
	state.follower = simulation.NewPetFollower(owner.ActiveCOS.GID, at)
	step := petCombatStep{
		key:   petOwnerKey{division: testDivision, name: owner.Name, gid: owner.ActiveCOS.GID},
		state: state, snapshot: owner, pet: owner.ActiveCOS, ref: ref, nowMs: clock.NowMs(),
	}
	primary := petCombatTarget{combatTarget: combatTarget{gid: 999999, at: at}}
	skill := enterworld.SkillRow{ActionArea: enterworld.SkillOffensiveArea{Shape: 2, Radius: 20, MaxTargets: 3, Select: 24}}
	selectVictim := func() (bool, bool) {
		found, foundPet := false, false
		for _, target := range rt.petAreaTargets(step, primary, skill) {
			found = found || target.gid == enterworld.ObjectIDForCharacter(victim)
			foundPet = foundPet || target.gid == pet.GID
		}
		return found, foundPet
	}

	copied := []string{}
	rt.companionCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
		copied = append(copied, character.Name)
		return character.SnapshotCompanion()
	}
	setCape(owner, 1)
	setCape(victim, 2)
	if found, foundPet := selectVictim(); !found || !foundPet {
		t.Fatalf("different-cape victim not selected on the companion copy: player=%t companion=%t", found, foundPet)
	}
	seen := false
	for _, name := range copied {
		seen = seen || name == victim.Name
	}
	if !seen {
		t.Fatalf("area selector never took the companion copy of %s; copied %v", victim.Name, copied)
	}
	setCape(victim, 1)
	if found, foundPet := selectVictim(); found || foundPet {
		t.Fatalf("same-cape victim selected on the companion copy: player=%t companion=%t", found, foundPet)
	}

	rt.companionCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
		return rt.characterSnapshot(divisionID, character)
	}
	setCape(victim, 2)
	if found, foundPet := selectVictim(); !found || !foundPet {
		t.Fatalf("different-cape victim not selected on the full copy: player=%t companion=%t", found, foundPet)
	}
	setCape(victim, 1)
	if found, foundPet := selectVictim(); found || foundPet {
		t.Fatalf("same-cape victim selected on the full copy: player=%t companion=%t", found, foundPet)
	}
}
