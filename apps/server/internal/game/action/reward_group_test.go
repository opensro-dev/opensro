/*
===========================================================================

reward_group_test.go - tests for reward_group.go

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
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
