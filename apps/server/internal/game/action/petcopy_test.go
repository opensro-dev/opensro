/*
===========================================================================

petcopy_test.go - the pet tick reads nothing outside its narrow copy (#2030)

The pet tick took the owner's whole record per summoned pet per tick.
advancePet and the elemental eligibility walk now take the companion
copy (domain SnapshotCompanion): equipment and companion rows, no
plain bag rows, no avatar inventory. The
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

	"opensro.online/server/internal/domain"
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
TestElementalWalkReadsNothingOutsideTheCompanionCopy

Production keeps the wolf's record in a bag row behind a stuffed bag.
The eligibility walk must resolve it on the companion copy - which
keeps companion rows and drops plain ones - and produce the same step
as on the whole-record copy; the seam must see the copy happen.
================
*/
func TestElementalWalkReadsNothingOutsideTheCompanionCopy(t *testing.T) {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_WOLF_040", 90)
	rt.AttackPetExpansion = true
	roarField(t, rt, c, clock)
	gid, now := c.ActiveCOS.GID, clock.NowMs()
	movePetToBagRow(c, 100)
	stuffOwnerInventories(c)

	copied := []string{}
	rt.companionCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
		copied = append(copied, character.Name)
		return character.SnapshotCompanion()
	}
	narrow, ok := rt.wolfCompanion(testDivision, c, gid, now)
	if !ok {
		t.Fatal("wolf in a bag row lost eligibility on the companion copy")
	}
	if len(copied) == 0 || copied[0] != c.Name {
		t.Fatalf("the walk never took the companion copy of its owner; copied %v", copied)
	}
	if narrow.snapshot.AvatarInventory != nil {
		t.Fatal("companion copy kept the avatar inventory")
	}
	for _, row := range narrow.snapshot.MissionInventory {
		if row.Summon == nil && (row.Slot < 0 || row.Slot >= int64(domain.EquipmentSlotEnd)) {
			t.Fatalf("companion copy kept plain bag row %+v", row)
		}
	}

	rt.companionCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
		return rt.characterSnapshot(divisionID, character)
	}
	full, ok := rt.wolfCompanion(testDivision, c, gid, now)
	if !ok {
		t.Fatal("wolf in a bag row lost eligibility on the full copy")
	}
	if narrow.pet.GID != full.pet.GID || narrow.pet.Codename != full.pet.Codename || narrow.pet.CurrentHP != full.pet.CurrentHP {
		t.Fatalf("pet diverged: narrow %+v, full %+v", narrow.pet, full.pet)
	}
	narrowPets, fullPets := narrow.snapshot.Companions(), full.snapshot.Companions()
	if len(narrowPets) != len(fullPets) {
		t.Fatalf("companion lists diverged: narrow %d pets, full %d", len(narrowPets), len(fullPets))
	}
	for i := range narrowPets {
		if narrowPets[i].GID != fullPets[i].GID {
			t.Fatalf("companion %d diverged: narrow gid %d, full gid %d", i, narrowPets[i].GID, fullPets[i].GID)
		}
	}
}
