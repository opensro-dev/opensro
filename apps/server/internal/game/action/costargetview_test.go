/*
===========================================================================

costargetview_test.go - a COS target's owner is the pet-owner view (#2030)

resolveCOSTarget detached its owner's whole record for every attack on a
COS and for every pet of every player an area query reached. It now
detaches the pet-owner view (domain SnapshotCompanion). The owner's
relations - body status, level, PK, aggressions, party, guild war, the
cape and job suits, a ridden transport's cargo - are judged twice per
case on an owner fattened with plain bag rows and an avatar row: once on
the view and once forced onto the whole copy. Every answer must agree; a
reader that reached a dropped row through the view would diverge.

===========================================================================
*/
package action

import (
	"fmt"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/guildwar"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
cosViewCase

One owner/attacker relation the COS target is judged under.
================
*/
type cosViewCase struct {
	name  string
	setup func(t *testing.T, rt *Runtime, attacker, owner *enterworld.Character)
}

/*
================
cosViewSuit

Wears a relation suit (job or free-battle cape) in the job-suit slot,
the one equipment row the view keeps for relations.
================
*/
func cosViewSuit(t *testing.T, rt *Runtime, c *enterworld.Character, codename string, tid4 int64, group float64) {
	t.Helper()
	items := rt.deps.ItemReferences().(cosTestItemSource).staticItemSource
	suit := &enterworld.ItemRef{RefObjID: uint32(9400 + tid4*10 + int64(group)), Codename: codename, Country: 3,
		TypeIDs: [4]int64{3, 1, 7, tid4}, ReqQuadTypes: [4]int64{-1, -1, -1, -1}, Combat: &enterworld.ItemCombatRef{},
		NativeFields: enterworld.NewNativeFields(map[string]float64{freeBattleGroupField: group})}
	items[suit.Codename] = suit
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
		Slot: int64(jobSuitSlot), RefObjID: suit.RefObjID, Codename: suit.Codename, TypeFlags: suit.TypeFlags(), StackCount: 1})
}

var cosViewCases = []cosViewCase{
	{name: "plain", setup: func(*testing.T, *Runtime, *enterworld.Character, *enterworld.Character) {}},
	{name: "owner protected", setup: func(_ *testing.T, _ *Runtime, _, owner *enterworld.Character) {
		owner.NativeBodyStatus = mercenaryBodyProtectedFirst
	}},
	{name: "owner below the level floor", setup: func(_ *testing.T, _ *Runtime, _, owner *enterworld.Character) {
		owner.Level = testInt64(5)
	}},
	{name: "owner murderer", setup: func(_ *testing.T, _ *Runtime, _, owner *enterworld.Character) {
		owner.PK = &domain.PKRecord{Penalty: 10}
	}},
	{name: "owner aggressor", setup: func(_ *testing.T, _ *Runtime, attacker, owner *enterworld.Character) {
		owner.Aggressions = map[uint32]uint32{enterworld.ObjectIDForCharacter(attacker) + 99: 30}
	}},
	{name: "attacker fighting the owner", setup: func(_ *testing.T, _ *Runtime, attacker, owner *enterworld.Character) {
		attacker.Aggressions = map[uint32]uint32{enterworld.ObjectIDForCharacter(owner): 30}
	}},
	{name: "same party", setup: func(_ *testing.T, rt *Runtime, attacker, owner *enterworld.Character) {
		gids := []uint32{enterworld.ObjectIDForCharacter(attacker), enterworld.ObjectIDForCharacter(owner)}
		rt.RewardParties = func(string) []RewardParty { return []RewardParty{{Members: gids}} }
	}},
	{name: "guilds at war", setup: func(t *testing.T, rt *Runtime, attacker, owner *enterworld.Character) {
		var err error
		if rt.GuildWars, err = guildwar.New(testDivision, warRelationStore{}); err != nil {
			t.Fatal(err)
		}
		attacker.GuildID, owner.GuildID = testInt64(7), testInt64(8)
	}},
	{name: "opposing capes", setup: func(t *testing.T, rt *Runtime, attacker, owner *enterworld.Character) {
		cosViewSuit(t, rt, attacker, "ITEM_TEST_CAPE_RED", 5, 1)
		cosViewSuit(t, rt, owner, "ITEM_TEST_CAPE_BLUE", 5, 2)
	}},
	{name: "same cape", setup: func(t *testing.T, rt *Runtime, attacker, owner *enterworld.Character) {
		cosViewSuit(t, rt, attacker, "ITEM_TEST_CAPE_RED", 5, 1)
		cosViewSuit(t, rt, owner, "ITEM_TEST_CAPE_RED", 5, 1)
	}},
	{name: "thief on a hunter", setup: func(t *testing.T, rt *Runtime, attacker, owner *enterworld.Character) {
		cosViewSuit(t, rt, attacker, "ITEM_TEST_THIEF", 2, 0)
		cosViewSuit(t, rt, owner, "ITEM_TEST_HUNTER", 3, 0)
	}},
	{name: "ridden", setup: func(_ *testing.T, _ *Runtime, _, owner *enterworld.Character) {
		owner.ActiveCOS.Mounted = true
	}},
}

/*
================
cosViewTrace

Every answer the owner's copy feeds, for one case: the resolution, the
attack validation under each control flag, the pet lanes' world enemy
test and the area candidates.
================
*/
func cosViewTrace(t *testing.T, c cosViewCase, full bool) string {
	t.Helper()
	rt, clock, attacker, owner := newPvpPair(t)
	rt.RewardActorPresent = func(string, string) bool { return true }
	equipStrikeTestCOS(t, rt, owner)
	petPeriodicFatten(owner)
	owner.AvatarInventory = &enterworld.AvatarInventory{Rows: []enterworld.InventoryRow{{Slot: 0, Codename: "ITEM_AVATAR_TEST"}}}
	c.setup(t, rt, attacker, owner)
	if full {
		rt.companionCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
			return rt.characterSnapshot(divisionID, character)
		}
	}
	now := clock.NowMs()
	target, ok := rt.resolveCombatTarget(testDivision, attacker, owner.ActiveCOS.GID, now)
	if !ok || target.cos == nil || target.snapshot == nil {
		t.Fatalf("%s: the COS does not resolve; the trace would agree vacuously", c.name)
	}
	if !full && target.snapshot.AvatarInventory != nil {
		t.Fatalf("%s: the target's owner is not the pet-owner view", c.name)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "target %d at %+v hp %d\n", target.gid, target.at, target.cos.CurrentHP)
	for _, flags := range []uint8{0, playerAttackArea, petAttackArea} {
		fmt.Fprintf(&b, "refusal %d %#x\n", flags, rt.cosTargetRefusalWith(testDivision, attacker, target, now, flags))
	}
	fmt.Fprintf(&b, "enemy %v\n", rt.worldPlayerEnemy(testDivision, attacker, target.snapshot))
	q := areaQuery{selects: areaSelectHostile, division: testDivision, caster: attacker,
		skill:  enterworld.SkillRow{Attack: enterworld.SkillAttack{Present: true}},
		center: rt.liveSpawn(simulation.WorldKey(testDivision, attacker.Name), attacker, now), reach: 50, now: now}
	for _, candidate := range rt.areaPlayerCandidates(q) {
		fmt.Fprintf(&b, "candidate %d cos %v at %.3f\n", candidate.target.gid, candidate.target.cos != nil, candidate.distance)
	}
	return b.String()
}

/*
================
TestCOSTargetOwnerViewAgreesWithTheWholeCopy
================
*/
func TestCOSTargetOwnerViewAgreesWithTheWholeCopy(t *testing.T) {
	refusals := map[string]bool{}
	for _, c := range cosViewCases {
		narrow, full := cosViewTrace(t, c, false), cosViewTrace(t, c, true)
		if narrow != full {
			t.Fatalf("%s diverged\nview:  %s\nwhole: %s", c.name, narrow, full)
		}
		refusals[strings.SplitN(strings.SplitN(narrow, "refusal 0 ", 2)[1], "\n", 2)[0]] = true
	}
	// The cases must reach more than one verdict, or the agreement says
	// nothing about the relation readers.
	if len(refusals) < 3 {
		t.Fatalf("the cases reached only the direct verdicts %v", refusals)
	}
}

/*
================
TestCOSTargetCaravanOwnerViewAgreesWithTheWholeCopy

The level-1 caravan protection reads the ridden transport's cargo off
the owner's copy: a dressed thief is refused 0x3024 against the trader's
loaded transport on the view as on the whole copy.
================
*/
func TestCOSTargetCaravanOwnerViewAgreesWithTheWholeCopy(t *testing.T) {
	codes := [2]uint16{}
	for i, full := range []bool{false, true} {
		rt, _, trader := caravanFixture(t)
		petPeriodicFatten(trader)
		thief := *trader
		thief.ID, thief.Name, thief.ActiveCOS = trader.ID+1, "thief", nil
		// Own rows: a shared backing array would let the thief's suit
		// overwrite the trader's.
		thief.MissionInventory = append([]enterworld.InventoryRow(nil), trader.MissionInventory...)
		cosViewSuit(t, rt, trader, "ITEM_CH_M_TRADE_TRADER_05", 1, 0)
		cosViewSuit(t, rt, &thief, "ITEM_CH_M_TRADE_THIEF_05", 2, 0)
		for _, c := range []*enterworld.Character{trader, &thief} {
			rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) },
				func(w *simulation.WorldState) {
					w.Spawn = simulation.Spawn{RegionID: pvpRegion, X: 960, Y: 20, Z: 458}
					w.SpawnSet = true
				})
		}
		if full {
			rt.companionCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
				return rt.characterSnapshot(divisionID, character)
			}
		}
		target, ok := rt.resolveCombatTarget(testDivision, &thief, trader.ActiveCOS.GID, 0)
		if !ok || target.cos == nil {
			t.Fatal("the loaded transport does not resolve")
		}
		codes[i] = rt.cosTargetRefusal(testDivision, &thief, target, 0)
	}
	if codes[0] != 0x3024 || codes[1] != 0x3024 {
		t.Fatalf("thief on a level-1 caravan's transport: view %#x, whole copy %#x, want 0x3024 on both", codes[0], codes[1])
	}
}

/*
================
TestAreaCandidatesTakeThePetOwnerView

The allocation guard: an area query over a player with a summoned COS
and a bag full of plain rows must save at least half of what the whole
copy costs over the view, measured against the same query forced onto
the whole copy (the companionCopy seam). Restoring the whole copy in
resolveCOSTarget (the mutation this guards) saves nothing and fails here.
================
*/
func TestAreaCandidatesTakeThePetOwnerView(t *testing.T) {
	rt, clock, attacker, owner := newPvpPair(t)
	rt.RewardActorPresent = func(string, string) bool { return true }
	equipStrikeTestCOS(t, rt, owner)
	petPeriodicFatten(owner)
	owner.AvatarInventory = &enterworld.AvatarInventory{Rows: []enterworld.InventoryRow{{Slot: 0, Codename: "ITEM_AVATAR_TEST"}}}
	now := clock.NowMs()
	q := areaQuery{selects: areaSelectHostile, division: testDivision, caster: attacker,
		skill:  enterworld.SkillRow{Attack: enterworld.SkillAttack{Present: true}},
		center: rt.liveSpawn(simulation.WorldKey(testDivision, attacker.Name), attacker, now), reach: 50, now: now}
	found := false
	for _, candidate := range rt.areaPlayerCandidates(q) {
		found = found || candidate.target.cos != nil
	}
	if !found {
		t.Fatal("the COS is not an area candidate; the guard would measure nothing")
	}
	bytesPerRun := func(run func()) int64 {
		return testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				run()
			}
		}).AllocedBytesPerOp()
	}
	saving := bytesPerRun(func() { owner.Snapshot() }) - bytesPerRun(func() { owner.SnapshotCompanion() })
	narrow := bytesPerRun(func() { rt.areaPlayerCandidates(q) })
	rt.companionCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
		return rt.characterSnapshot(divisionID, character)
	}
	full := bytesPerRun(func() { rt.areaPlayerCandidates(q) })
	t.Logf("area query %d B/run on the view, %d B/run on the whole copy; the copies differ by %d B", narrow, full, saving)
	if full-narrow < saving/2 {
		t.Fatalf("the area query saves %d B/run over the whole copy, want at least %d (half the copies' difference)", full-narrow, saving/2)
	}
}
