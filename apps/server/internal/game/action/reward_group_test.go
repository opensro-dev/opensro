/*
===========================================================================

reward_group_test.go - tests for reward_group.go

===========================================================================
*/

package action

import (
	"math"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"testing"
)

func TestRewardRangeUsesNativePlanarAndDungeonCoordinates(t *testing.T) {
	a := simulation.Spawn{RegionID: 0x6288, X: 1900, Y: -9000}
	b := simulation.Spawn{RegionID: 0x6289, X: 980, Y: 9000}
	if !withinPartyRewardRange(a, b) {
		t.Fatal("inclusive 1000-unit outdoor boundary or Y ignored incorrectly")
	}
	b.X++
	if withinPartyRewardRange(a, b) {
		t.Fatal("outside boundary admitted")
	}
	a = simulation.Spawn{RegionID: 0x8001, X: 0}
	b = simulation.Spawn{RegionID: 0x8002, X: 1000}
	if !withinPartyRewardRange(a, b) {
		t.Fatal("dungeon local coordinates treated as outdoor sectors")
	}
	b.RegionID = 2
	if withinPartyRewardRange(a, b) {
		t.Fatal("different planes admitted")
	}
}

func TestRewardGroupsUsePartyObjectOrderAndGIDRepresentativeTies(t *testing.T) {
	p := &RewardParty{Order: 20, Options: 1, Members: []uint32{9, 2}}
	q := &RewardParty{Order: 10, Options: 1, Members: []uint32{4}}
	actors := map[uint32]rewardActor{
		9: {character: &enterworld.Character{}, party: p},
		2: {character: &enterworld.Character{}, party: p},
		4: {character: &enterworld.Character{}, party: q},
	}
	groups := monsterRewardGroups([]simulation.MonsterContribution{{CreditGID: 9, Damage: 40}, {CreditGID: 4, Damage: 80}, {CreditGID: 2, Damage: 40}, {CreditGID: 99, Damage: 999}}, actors)
	if len(groups) != 2 || groups[0].order != 10 || groups[0].damage != 80 || groups[1].representative != 2 || groups[1].damage != 80 {
		t.Fatalf("group order, unresolved source, or representative tie: %+v", groups)
	}
}

func TestFatalActorDoesNotStealRewardOrDropOwnership(t *testing.T) {
	rt, _, actor, target := newCombatTestRuntime(t, 100)
	peer := *actor
	peer.ID, peer.Name = 4, "contributor"
	deps := rt.deps.(*enterworld.Deps)
	source := fixtureCharacters(deps.Characters)
	source[testDivision] = append(source[testDivision], &peer)
	gid := enterworld.ObjectIDForCharacter(&peer)
	hits := rt.Monsters.ApplyDamageSequence(testDivision, target.Gid, 100, []simulation.MonsterDamagePlan{{GID: target.Gid, Damage: 99, CreditGID: gid}})
	if len(hits) != 1 || hits[0].Fatal {
		t.Fatal("preparation failed")
	}
	installSmallGoldRef(rt)
	rt.DropRoll = goldOnlyMonsterDropRoll(0, 0)
	rewards := map[int64]int64{}
	rt.UpdateExperience = func(c *enterworld.Character, exp, sexp int64, defeated uint32) ([]wire.Frame, bool) {
		rewards[c.ID] += exp
		return []wire.Frame{{Opcode: wire.OpExpUpdate, Payload: []byte{byte(c.ID)}}}, true
	}
	result := rt.HandleTargetInteract(testDivision, actor, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	if rewards[peer.ID] == 0 || rewards[actor.ID] == 0 {
		t.Fatalf("not all contributors paid: %v; %+v", rewards, result)
	}
	drops := rt.Ground.All(testDivision)
	if len(drops) != 1 || drops[0].OwnerJID != gid {
		t.Fatalf("fatal actor stole drops: %+v", drops)
	}
	if len(result.Recipients) != 1 || result.Recipients[0].CharacterID != peer.ID {
		t.Fatalf("missing private contributor delivery: %+v", result.Recipients)
	}
}

/*
================
TestQuestKillCreditsTheFatalHitPlayer

The original raises the quest kill event for the player whose blow killed
the monster. The contributor who dealt 99 of 100 keeps the loot, but the
quest kill and the quest drop roll belong to the killing blow.
================
*/
func TestQuestKillCreditsTheFatalHitPlayer(t *testing.T) {
	rt, _, actor, target := newCombatTestRuntime(t, 100)
	peer := *actor
	peer.ID, peer.Name = 4, "contributor"
	deps := rt.deps.(*enterworld.Deps)
	source := fixtureCharacters(deps.Characters)
	source[testDivision] = append(source[testDivision], &peer)
	gid := enterworld.ObjectIDForCharacter(&peer)
	if hits := rt.Monsters.ApplyDamageSequence(testDivision, target.Gid, 100,
		[]simulation.MonsterDamagePlan{{GID: target.Gid, Damage: 99, CreditGID: gid}}); len(hits) != 1 || hits[0].Fatal {
		t.Fatal("preparation failed")
	}
	installSmallGoldRef(rt)
	rt.DropRoll = goldOnlyMonsterDropRoll(0, 0)
	var quested, rolled []string
	rt.UpdateQuestKill = func(c *enterworld.Character, _ string, _ uint8) ([]wire.Frame, bool) {
		quested = append(quested, c.Name)
		return nil, true
	}
	rt.QuestMonsterDrops = func(c *enterworld.Character, _ string, _ uint8, _ func() (uint32, error)) []inventory.ItemAmount {
		rolled = append(rolled, c.Name)
		return nil
	}
	rt.HandleTargetInteract(testDivision, actor, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	if len(quested) != 1 || quested[0] != actor.Name || len(rolled) != 1 || rolled[0] != actor.Name {
		t.Fatalf("quest kill %v, quest drop roll %v, want only the killing blow %q", quested, rolled, actor.Name)
	}
	if drops := rt.Ground.All(testDivision); len(drops) != 1 || drops[0].OwnerJID != gid {
		t.Fatalf("the loot left the top contributor: %+v", drops)
	}
}

func TestPartyKillPaysNearbyIdleMemberButExcludesDeadAndOffline(t *testing.T) {
	rt, _, actor, target := newCombatTestRuntime(t, 1)
	deps := rt.deps.(*enterworld.Deps)
	source := fixtureCharacters(deps.Characters)
	members := []uint32{enterworld.ObjectIDForCharacter(actor)}
	for i := int64(4); i <= 6; i++ {
		c := *actor
		c.ID = i
		c.Name = map[int64]string{4: "idle", 5: "dead", 6: "offline"}[i]
		if i == 5 {
			c.CurrentHP = testInt64(0)
		}
		source[testDivision] = append(source[testDivision], &c)
		members = append(members, enterworld.ObjectIDForCharacter(&c))
	}
	rt.RewardParties = func(string) []RewardParty { return []RewardParty{{Order: 1, Options: 1, Members: members}} }
	rt.RewardActorPresent = func(_ string, name string) bool { return name != "offline" }
	rewards := map[int64]int64{}
	rt.UpdateExperience = func(c *enterworld.Character, exp, sexp int64, _ uint32) ([]wire.Frame, bool) {
		rewards[c.ID] += exp
		return []wire.Frame{{Opcode: wire.OpExpUpdate}}, true
	}
	result := rt.HandleTargetInteract(testDivision, actor, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	// Equal level Chinese members share a 1.1 pool: trunc(26 * .55)=14.
	if len(rewards) != 2 || rewards[actor.ID] != 14 || rewards[4] != 14 {
		t.Fatalf("party eligibility/reward: %v", rewards)
	}
	if len(result.Recipients) != 1 || result.Recipients[0].CharacterID != 4 {
		t.Fatalf("private sharing projection: %+v", result.Recipients)
	}
}

func TestBurnContinuesWithNoLiveRewardRoster(t *testing.T) {
	rt, clock, _, target := newCombatTestRuntime(t, 1)
	rt.RewardActorPresent = func(string, string) bool { return false }
	deps := rt.deps.(*enterworld.Deps)
	deps.UpdateCharacters = func(cs []*enterworld.Character, _ string, f func() bool) bool {
		if len(cs) == 0 {
			t.Fatal("empty roster sent to character transaction")
		}
		return f()
	}
	seedDepartedAbnormal(t, rt, actorOf(t, rt), target.Gid, abnormal.Record{Status: abnormal.Burn, Level: 100, DurationMs: 100 * 750, Rate24: 8, Scale20: 1})
	rt.advanceMonsterAbnormals(clock.NowMs())
	after, _ := rt.Monsters.Get(testDivision, target.Gid)
	if after.CurrentHP != 0 {
		t.Fatalf("unattributed burn stalled at %d HP", after.CurrentHP)
	}
}

func TestPartyRewardRejectsOtherInstanceAtIdenticalCoordinates(t *testing.T) {
	rt, _, actor, target := newCombatTestRuntime(t, 1)
	peer := *actor
	peer.ID, peer.Name = 4, "other-instance"
	world := *actor.World
	packed := uint32(0x20001)
	world.PackedInstance = &packed
	peer.World = &world
	source := fixtureCharacters(rt.deps.(*enterworld.Deps).Characters)
	source[testDivision] = append(source[testDivision], &peer)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Order: 1, Options: 1, Members: []uint32{enterworld.ObjectIDForCharacter(actor), enterworld.ObjectIDForCharacter(&peer)}}}
	}
	rt.UpdateExperience = func(c *enterworld.Character, _, _ int64, _ uint32) ([]wire.Frame, bool) {
		if c.ID == peer.ID {
			t.Fatal("reward crossed native world-instance identity")
		}
		return nil, true
	}
	rt.HandleTargetInteract(testDivision, actor, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
}

/*
================
TestPartyShareFloor

A level 1 beside a level 23 takes 1/24 of the shared kill natively
(reference server); with the beta floor it takes an even half, and the level 23
keeps its level share.
================
*/
func TestPartyShareFloor(t *testing.T) {
	one, high := int64(1), int64(23)
	members := []rewardActor{
		{character: &enterworld.Character{Level: &one}},
		{character: &enterworld.Character{Level: &high}},
	}
	native := partyRewardFactors(members, monster.Instance{}, false)
	floored := partyRewardFactors(members, monster.Instance{}, true)
	// The party bonus multiplies every share alike.
	bonus := float64(native[1]) * 24 / 23
	if math.Abs(float64(native[0])-bonus/24) > 1e-6 {
		t.Fatalf("native level-1 factor %v, want 1/24 x %v", native[0], bonus)
	}
	if math.Abs(float64(floored[0])-bonus/2) > 1e-6 || floored[1] != native[1] {
		t.Fatalf("floored factors %v, native %v", floored, native)
	}
}

/*
================
TestPetBurstKillCreditsNoQuest

The port-only pet bursts (pet shield, wolf hunt) land their hit in the
owner's name through the credited-hit lane. Their fatal hit is still a
companion kill: no quest kill, where the owner's own blow credits one.
================
*/
func TestPetBurstKillCreditsNoQuest(t *testing.T) {
	for _, companion := range []bool{false, true} {
		rt, clock, c, mob := newCombatTestRuntime(t, 1)
		quested := 0
		rt.UpdateQuestKill = func(*enterworld.Character, string, uint8) ([]wire.Frame, bool) {
			quested++
			return nil, true
		}
		row := rt.deps.SkillData().(staticSkillSource)[2]
		snapshot := rt.characterSnapshot(testDivision, c)
		commit := rt.commitCreditedMonsterHit
		if companion {
			commit = rt.commitCompanionCreditedMonsterHit
		}
		hit, ok := commit(testDivision, c, snapshot, row, mob, combat.Result{Damage: 50, ResultFlags: 1}, "test-kill", clock.NowMs())
		if !ok || !hit.impacts[0].Fatal {
			t.Fatalf("companion=%v: the kill did not commit", companion)
		}
		if want := map[bool]int{false: 1, true: 0}[companion]; quested != want {
			t.Fatalf("companion=%v: %d quest kills, want %d", companion, quested, want)
		}
	}
}

/*
================
TestCreditedHitBuildsTheRewardRosterOnlyForAKill

The roster serves a kill alone (#2030): a surviving credited hit must
not build it, a killing one must. Presence queries witness the build -
the roster asks once per eligible character it materializes.
================
*/
func TestCreditedHitBuildsTheRewardRosterOnlyForAKill(t *testing.T) {
	rt, clock, c, mob := newCombatTestRuntime(t, 10000)
	presence := 0
	rt.RewardActorPresent = func(division, name string) bool {
		presence++
		return true
	}
	row := rt.deps.SkillData().(staticSkillSource)[2]
	snapshot := rt.characterSnapshot(testDivision, c)
	hit, ok := rt.commitCreditedMonsterHit(testDivision, c, snapshot, row, mob, combat.Result{Damage: 50, ResultFlags: 1}, "test-hit", clock.NowMs())
	if !ok || len(hit.impacts) == 0 || hit.impacts[0].Fatal {
		t.Fatalf("the surviving hit did not commit plainly: %+v", hit)
	}
	if presence != 0 {
		t.Fatalf("a surviving hit built the reward roster (%d presence queries)", presence)
	}

	rt2, clock2, c2, doomed := newCombatTestRuntime(t, 1)
	presence2 := 0
	rt2.RewardActorPresent = func(division, name string) bool {
		presence2++
		return true
	}
	row2 := rt2.deps.SkillData().(staticSkillSource)[2]
	snapshot2 := rt2.characterSnapshot(testDivision, c2)
	hit2, ok := rt2.commitCreditedMonsterHit(testDivision, c2, snapshot2, row2, doomed, combat.Result{Damage: 50, ResultFlags: 1}, "test-kill", clock2.NowMs())
	if !ok || len(hit2.impacts) == 0 || !hit2.impacts[0].Fatal {
		t.Fatalf("the killing hit did not commit: %+v", hit2)
	}
	if presence2 == 0 {
		t.Fatal("the killing hit never built the reward roster")
	}
}

/*
================
TestRewardRosterResolvesPosesWhenTheyAreRead

#2030: the roster build resolves no positions; a pose read sees the
actor where it stands at the read, not where it stood at the build.
================
*/
func TestRewardRosterResolvesPosesWhenTheyAreRead(t *testing.T) {
	rt, _, actor, _ := newCombatTestRuntime(t, 100)
	roster := rt.monsterRewardRoster(testDivision, actor, 1000)
	a, ok := roster.actors[enterworld.ObjectIDForCharacter(actor)]
	if !ok {
		t.Fatal("the actor is missing from its own roster")
	}
	before := rt.rewardActorPose(testDivision, a, 1000)
	rt.Worlds.Update(simulation.WorldKey(testDivision, actor.Name),
		func() simulation.WorldState { panic("the actor world must exist") },
		func(w *simulation.WorldState) { w.Spawn.X = before.X + 5000 })
	after := rt.rewardActorPose(testDivision, a, 1000)
	if after.X != before.X+5000 {
		t.Fatalf("the pose was frozen at roster build: before %+v, after %+v", before, after)
	}
}

/*
================
TestSkillStrikeBuildsTheRewardRosterOnlyForAKill

#2030 strike gate: a skill strike whose planned damage cannot kill
doors the caster alone and builds no reward roster; a killing strike
keeps the roster-wide door. Presence queries witness the build, as
in the credited lane's pin.
================
*/
func TestSkillStrikeBuildsTheRewardRosterOnlyForAKill(t *testing.T) {
	rt, _, actor, target := newCombatTestRuntime(t, 10000)
	presence := 0
	rt.RewardActorPresent = func(division, name string) bool {
		presence++
		return true
	}
	rt.HandleTargetInteract(testDivision, actor, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	if presence != 0 {
		t.Fatalf("a strike that cannot kill built the reward roster (%d presence queries)", presence)
	}
	if got := rt.surpriseFatalCount.Load(); got != 0 {
		t.Fatalf("a plain strike counted %d surprise fatals", got)
	}

	rt2, _, actor2, doomed := newCombatTestRuntime(t, 1)
	presence2 := 0
	rt2.RewardActorPresent = func(division, name string) bool {
		presence2++
		return true
	}
	rt2.HandleTargetInteract(testDivision, actor2, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: doomed.Gid}.Encode())
	if presence2 == 0 {
		t.Fatal("a killing strike never built the reward roster")
	}
	if got := rt2.surpriseFatalCount.Load(); got != 0 {
		t.Fatalf("a killing strike counted %d surprise fatals", got)
	}
}

/*
================
TestSkillStrikeSurpriseFatalIsCounted

A fatal impact settled without a lethal plan means the planned-damage
invariant broke: the settlement still runs, and the break is counted
(and logged at Error by noteSurpriseFatal) instead of silent. A
lethal plan counts nothing.
================
*/
func TestSkillStrikeSurpriseFatalIsCounted(t *testing.T) {
	rt, _, actor, mob := newCombatTestRuntime(t, 100)
	impact := simulation.MonsterDamageResult{Instance: mob, Fatal: true}
	roster := rewardRoster{division: testDivision, characters: []*enterworld.Character{actor}}
	rt.settleStrikeFatal(testDivision, actor, roster, false, impact, monster.Pose{}, 1000)
	if got := rt.surpriseFatalCount.Load(); got != 1 {
		t.Fatalf("surprise fatal count = %d, want 1", got)
	}
	rt.settleStrikeFatal(testDivision, actor, roster, true, impact, monster.Pose{}, 1000)
	if got := rt.surpriseFatalCount.Load(); got != 1 {
		t.Fatalf("a lethal-plan kill counted as a surprise: %d", got)
	}
}
