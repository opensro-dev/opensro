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
	"opensro.online/server/internal/game/pk"
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
TestBondRevivalReadsNothingOutsideTheCompanionCopy

The bond revival weighs a dead party member through a detached copy:
the recipient gate and the combat stats that set the revival HP.
Production members carry stuffed bags; the stats walk reads
equipment rows only, so the revived HP must agree on the companion
copy and on the whole-record copy, and the seam must see the copy.
================
*/
func TestBondRevivalReadsNothingOutsideTheCompanionCopy(t *testing.T) {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
	roarField(t, rt, c, clock)
	c.MissionInventory = nil
	rt.AttackPetExpansion = true
	learnPetBook(t, c, 27)
	near := nearbyCharacter(rt, c, 21, "bond-near", 50)
	near.ActiveCOS = nil
	rt.BindPetSession(testDivision, near, 2)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(near)}}}
	}
	stuffOwnerInventories(near)
	narrowSnap, fullSnap := near.SnapshotCompanion(), near.Snapshot()
	narrowStats, _, narrowErr := rt.playerCombatStats(testDivision, narrowSnap)
	fullStats, _, fullErr := rt.playerCombatStats(testDivision, fullSnap)
	if narrowErr != nil || fullErr != nil {
		t.Fatalf("revival stats errors: companion %v, full %v", narrowErr, fullErr)
	}
	// Compare what the revival actually weighs: the vitals the stats
	// produce for this victim (Stats itself carries a graph pointer
	// that legitimately differs between any two calls).
	nHP, nMP, nCHP, nCMP := keeperVitalsFrom(narrowSnap, narrowStats, nil)
	fHP, fMP, fCHP, fCMP := keeperVitalsFrom(fullSnap, fullStats, nil)
	if nHP != fHP || nMP != fMP || nCHP != fCHP || nCMP != fCMP {
		t.Fatalf("revival vitals diverged on the fattened victim: narrow (%d,%d,%d,%d), full (%d,%d,%d,%d)",
			nHP, nMP, nCHP, nCMP, fHP, fMP, fCHP, fCMP)
	}
	key := petOwnerKey{testDivision, c.Name, c.ActiveCOS.GID}
	state := rt.petSessionFor(key.division, key.name, key.gid)

	revive := func() (int64, string) {
		near.DeathKind = uint8(pk.DeathMonster)
		near.CurrentHP = testInt64(0)
		c.ActiveCOS.Training.PassiveReadyAtMs[1] = 0
		frames := rt.advancePetBond(key, state, clock.NowMs())
		if *near.CurrentHP <= 0 || len(frames) == 0 {
			t.Fatal("dead party member was not revived; the comparison would agree vacuously")
		}
		return *near.CurrentHP, fmt.Sprintf("%+v", frames)
	}

	copied := []string{}
	rt.companionCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
		copied = append(copied, character.Name)
		return character.SnapshotCompanion()
	}
	narrowHP, narrowFrames := revive()
	seen := false
	for _, name := range copied {
		seen = seen || name == near.Name
	}
	if !seen {
		t.Fatalf("revival never took the companion copy of %s; copied %v", near.Name, copied)
	}

	rt.companionCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
		return rt.characterSnapshot(divisionID, character)
	}
	fullHP, fullFrames := revive()
	if narrowHP != fullHP {
		t.Fatalf("revival HP diverged: companion copy %d, full copy %d (revival stats read outside equipment?)", narrowHP, fullHP)
	}
	if narrowFrames != fullFrames {
		t.Fatalf("revival frames diverged\nnarrow: %.600s\nfull:   %.600s", narrowFrames, fullFrames)
	}
}
