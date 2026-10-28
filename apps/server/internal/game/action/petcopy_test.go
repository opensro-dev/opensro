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
	"sort"
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
petShieldTrace

A tier-2 party shield cast on a fattened owner, partly absorbed, then
advanced past expiry: the shield tick revalidates the owner and the
recipient through its copy and retires both shields. (The tier-3
burst settlement runs through the same copies and is covered by
TestPetShieldTiersAndExpiry and TestPetShieldBurstKillCreditsNoQuest;
its combat rolls make two fixture runs disagree with each other, so
it cannot anchor a trace.) Run narrow and forced-full: every batch
must agree, in any order - the tick walks its rows map.
================
*/
func petShieldTrace(t *testing.T, full bool) string {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
	roarField(t, rt, c, clock)
	c.MissionInventory = nil
	rt.AttackPetExpansion = true
	near := nearbyCharacter(rt, c, 21, "shield-near", 50)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(near)}}}
	}
	petPeriodicFatten(c)
	if full {
		rt.companionCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
			return rt.characterSnapshot(divisionID, character)
		}
	}
	skill := learnPetBook(t, c, 20)
	result := rt.castPetShield(testDivision, c, c, c.ActiveCOS, skill, clock.NowMs())
	if len(result.Broadcast) < 2 {
		t.Fatalf("no shields: %+v", result)
	}
	self := petShieldKey{testDivision, enterworld.ObjectIDForCharacter(c)}
	rt.absorbPetShield(testDivision, self.gid, rt.petShields.rows[self].amount-20, clock.NowMs())
	var lines []string
	for _, now := range []int64{clock.NowMs() + 4999, clock.NowMs() + 5000} {
		for _, batch := range rt.advancePetShields(now) {
			lines = append(lines, fmt.Sprintf("%+v", batch))
		}
	}
	if len(rt.petShields.rows) != 0 {
		t.Fatal("shield retained; the trace would agree vacuously")
	}
	if len(lines) == 0 {
		t.Fatal("shield tick produced no batches; the trace would agree vacuously")
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

/*
================
TestPetShieldTickReadsNothingOutsideTheCompanionCopy
================
*/
func TestPetShieldTickReadsNothingOutsideTheCompanionCopy(t *testing.T) {
	narrow, full := petShieldTrace(t, false), petShieldTrace(t, true)
	a, b := strings.Split(narrow, "\n"), strings.Split(full, "\n")
	if len(a) != len(b) {
		t.Fatalf("trace lengths diverged: narrow %d lines, full %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("line %d diverged\nnarrow: %.900s\nfull:   %.900s", i, a[i], b[i])
		}
	}
}

/*
================
TestPetShieldTickTakesTheCompanionCopy

Routing guard for the shield tick's two copies: the seam must fire
for the owner (and the recipient) on an advance, so a site reverted
to the whole-record copy fails here even though its frames agree.
================
*/
func TestPetShieldTickTakesTheCompanionCopy(t *testing.T) {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
	roarField(t, rt, c, clock)
	c.MissionInventory = nil
	rt.AttackPetExpansion = true
	skill := learnPetBook(t, c, 20)
	if result := rt.castPetShield(testDivision, c, c, c.ActiveCOS, skill, clock.NowMs()); len(result.Broadcast) < 2 {
		t.Fatalf("no shields: %+v", result)
	}
	copies := 0
	rt.companionCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
		copies++
		return rt.characterSnapshot(divisionID, character)
	}
	rt.advancePetShields(clock.NowMs() + 5000)
	if copies == 0 {
		t.Fatal("the shield tick never took the companion copy (whole-record copy back?)")
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
