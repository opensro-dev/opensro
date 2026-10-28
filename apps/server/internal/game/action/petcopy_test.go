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
TestTailwindRecipientsReadNothingOutsideTheCompanionCopy

The Tailwind recipient pass copies every admitted party member to
gate it; production members carry stuffed bags. A fattened member
must charge exactly as on the whole-record copy, and the seam must
see the member's copy happen.
================
*/
func TestTailwindRecipientsReadNothingOutsideTheCompanionCopy(t *testing.T) {
	rt, clock, c, _, key := tailwindFixture(t, 3)
	member := nearbyCharacter(rt, c, 21, "tailwind-party", 50)
	memberGID := enterworld.ObjectIDForCharacter(member)
	rt.petSessions[petOwnerKey{division: testDivision, name: member.Name}] = &petSession{character: member, ready: true}
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), memberGID}}}
	}
	stuffOwnerInventories(member)

	copied := []string{}
	rt.companionCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
		copied = append(copied, character.Name)
		return character.SnapshotCompanion()
	}
	rt.advanceTailwind(key, clock.NowMs())
	chargeTailwind(rt, clock, key, 10)
	if got := rt.tailwindView(testDivision, memberGID, clock.NowMs()).stacks; got != 10 {
		t.Fatalf("party member charged %d stacks on the companion copy, want 10", got)
	}
	found := false
	for _, name := range copied {
		if name == member.Name {
			found = true
		}
	}
	if !found {
		t.Fatalf("recipient pass never took the companion copy of %s; copied %v", member.Name, copied)
	}

	rt.tailwinds.sources = nil
	rt.companionCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
		return rt.characterSnapshot(divisionID, character)
	}
	rt.advanceTailwind(key, clock.NowMs())
	chargeTailwind(rt, clock, key, 10)
	if got := rt.tailwindView(testDivision, memberGID, clock.NowMs()).stacks; got != 10 {
		t.Fatalf("party member charged %d stacks on the full copy, want 10", got)
	}
}
