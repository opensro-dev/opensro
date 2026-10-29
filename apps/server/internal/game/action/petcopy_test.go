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

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
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
petCounterTrace

Ten grouped hits charge the bear counter and the tenth stuns, on a
scripted roll (draw 4999, fully deterministic). The owner record is
fattened after the fixture's setup: the bear lives in a bag row, so
the counter resolves it through the companion walk either way.
================
*/
func petCounterTrace(t *testing.T, full bool) string {
	rt, clock, c, original := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
	rt.AttackPetExpansion = true
	ref := original.Ref
	ref.Level = 90
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{ref.RefObjID: ref}, nil))
	rt.Monsters.SetAbnormalContext(monsterAbnormalContext{rt})
	rt.Monsters.SetTimeSource(clock.Now)
	rt.Monsters.StartDivision(testDivision)
	first := roarField(t, rt, c, clock)
	learnPetBook(t, c, 30)
	at := rt.LiveSpawnFor(testDivision, c)
	lease, _ := rt.casterPopulation(testDivision, c)
	gids := []uint32{first.Gid}
	for _, distance := range []float64{40, 50, 60, 70, 81} {
		point := at
		point.X += distance
		rt.Monsters.SpawnGMMonsters(simulation.GMMonsterSpawn{Division: testDivision, Population: lease, RefObjID: ref.RefObjID, Count: 1, Position: point, NowMs: clock.NowMs()})
		rows := rt.Monsters.CombatCandidatesInPopulation(testDivision, lease, point, 1, clock.NowMs(), true)
		if len(rows) != 1 {
			t.Fatal("missing group fixture")
		}
		gids = append(gids, rows[0].Gid)
	}
	state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
	bearAt := at
	bearAt.X += 300
	state.follower = simulation.NewPetFollower(c.ActiveCOS.GID, bearAt)
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 13, Summon: c.ActiveCOS}}
	c.ActiveCOS = nil
	petPeriodicFatten(c)
	rt.CombatRoll = func() (uint32, error) { return 4999, nil }
	if full {
		rt.companionCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
			return rt.characterSnapshot(divisionID, character)
		}
	}
	hit := petCounterHit{division: testDivision, owner: c, victim: enterworld.ObjectIDForCharacter(c), formulas: []combat.Result{{Damage: 1}}, now: clock.NowMs()}
	trace := ""
	for i := 0; i < 9; i++ {
		hit.attacker = gids[i%4]
		trace += fmt.Sprintf("%+v\n", rt.petCounterStun(hit))
	}
	hit.attacker = gids[4]
	trace += fmt.Sprintf("%+v\n", rt.petCounterStun(hit))
	stunned := 0
	for _, gid := range gids {
		mob, _ := rt.Monsters.Get(testDivision, gid)
		if mob.Abnormal != nil && mob.Abnormal.Slots[abnormal.Stun].Active {
			stunned++
		}
	}
	if stunned == 0 {
		t.Fatal("counter never stunned; the trace would agree vacuously")
	}
	return trace + fmt.Sprintf("stunned=%d hits=%v", stunned, state.trained.counterHits)
}

/*
================
TestPetCounterReadsNothingOutsideTheCompanionCopy
================
*/
func TestPetCounterReadsNothingOutsideTheCompanionCopy(t *testing.T) {
	narrow, full := petCounterTrace(t, false), petCounterTrace(t, true)
	if narrow != full {
		t.Fatalf("counter traces diverged\nnarrow: %.900s\nfull:   %.900s", narrow, full)
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
	near := nearbyCharacter(rt, c, 21, "shield-near", 50)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(near)}}}
	}
	skill := learnPetBook(t, c, 20)
	if result := rt.castPetShield(testDivision, c, c, c.ActiveCOS, skill, clock.NowMs()); len(result.Broadcast) < 2 {
		t.Fatalf("no shields: %+v", result)
	}
	copied := map[string]int{}
	rt.companionCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
		copied[character.Name]++
		return rt.characterSnapshot(divisionID, character)
	}
	rt.advancePetShields(clock.NowMs() + 5000)
	// The owner site sits inside the per-row loop (once per shield row), and
	// each row then copies its own recipient at the recipient site, so the
	// owner's own row also copies him there. Both sites therefore leave a
	// distinct shape: recipient copies of the other member exist at all, and
	// owner copies outnumber them. A whole-record copy back at either site
	// breaks one of the two.
	if copied[near.Name] == 0 {
		t.Fatalf("the shield tick never companion-copied recipient %s (whole-record copy back at the recipient site?); copied %v", near.Name, copied)
	}
	if copied[c.Name] <= copied[near.Name] {
		t.Fatalf("owner companion copies = %d, recipient copies = %d; the owner site must copy the owner once per shield row (whole-record copy back there?); copied %v", copied[c.Name], copied[near.Name], copied)
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
