/*
===========================================================================

pvp_test.go - players striking players, and what a kill costs and pays

The pair stands in a PvP region (0x62AA) beside each other; the attacker
already holds the victim as an aggressor, which 5293A0 admits before its
level gates, so the level-1 fixture rows stay valid.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// pvpRegion is a field region whose region record permits player combat.
const pvpRegion = uint16(0x62aa)

/*
================
newPvpPair

The combat fixture's character as the attacker and a copy of it as the
victim three units away, both in pvpRegion.
================
*/
func newPvpPair(t *testing.T) (*Runtime, *fakeClock, *enterworld.Character, *enterworld.Character) {
	t.Helper()
	rt, clock, a, _ := newCombatTestRuntime(t, 100000)
	v := *a
	v.ID, v.Name = 4, "victim"
	v.CurrentHP = testInt64(100)
	v.MissionInventory = append([]enterworld.InventoryRow(nil), a.MissionInventory...)
	world := *a.World
	spawn := *world.Spawn
	world.Spawn = &spawn
	v.World = &world
	deps := rt.deps.(*enterworld.Deps)
	fixtureCharacters(deps.Characters)[testDivision] = append(fixtureCharacters(deps.Characters)[testDivision], &v)
	for i, c := range []*enterworld.Character{a, &v} {
		dx := float64(i) * 3
		rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) },
			func(w *simulation.WorldState) {
				w.Spawn = simulation.Spawn{RegionID: pvpRegion, X: 960 + dx, Y: 20, Z: 458}
				w.SpawnSet = true
			})
	}
	a.Aggressions = map[uint32]uint32{enterworld.ObjectIDForCharacter(&v): playerAggressionTicks}
	return rt, clock, a, &v
}

/*
================
TestPlayerBasicAttackStrikesPlayer
================
*/
func TestPlayerBasicAttackStrikesPlayer(t *testing.T) {
	rt, _, a, v := newPvpPair(t)
	victim := enterworld.ObjectIDForCharacter(v)
	out := rt.HandleTargetInteract(testDivision, a, wire.BasicAttackEngage{TargetGid: victim}.Encode())
	if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok {
		t.Fatalf("no cast result: %+v (%s)", out.Frames, out.DiagnosticRefusal)
	}
	if *v.CurrentHP >= 100 {
		t.Fatalf("victim HP %d, want below 100", *v.CurrentHP)
	}
	delivered := false
	for _, r := range out.Recipients {
		delivered = delivered || r.CharacterID == v.ID
	}
	if !delivered {
		t.Fatal("the victim's private frames were not addressed to it")
	}
}

/*
================
TestPlayerAttackRefusesPartyMember

5293A0: party protection precedes every other relation.
================
*/
func TestPlayerAttackRefusesPartyMember(t *testing.T) {
	rt, _, a, v := newPvpPair(t)
	members := []uint32{enterworld.ObjectIDForCharacter(a), enterworld.ObjectIDForCharacter(v)}
	rt.RewardParties = func(string) []RewardParty { return []RewardParty{{Members: members}} }
	out := rt.HandleTargetInteract(testDivision, a, wire.BasicAttackEngage{TargetGid: members[1]}.Encode())
	refusal, ok := findFrame(out.Frames, wire.OpSkillCastResult)
	if !ok || len(refusal.Payload) != 2 || refusal.Payload[0] != 2 || refusal.Payload[1] != 0x22 {
		t.Fatalf("party member attack answer: %+v", out.Frames)
	}
	if *v.CurrentHP != 100 {
		t.Fatal("a party member took damage")
	}
}

/*
================
TestMurderBooksTheKiller

4E1F60 kind 3: total +1, penalty (total+1)/2*1200, daily +1 at an even
level, and the PvP EXP of 4105D0 (two level-1 players: 24 * 1.09 = 26).
================
*/
func TestMurderBooksTheKiller(t *testing.T) {
	rt, _, a, v := newPvpPair(t)
	v.CurrentHP = testInt64(1)
	var granted int64
	rt.UpdateExperience = func(c *enterworld.Character, exp, _ int64, _ uint32) ([]wire.Frame, bool) {
		if c == a {
			granted += exp
		}
		return nil, true
	}
	rt.HandleTargetInteract(testDivision, a, wire.BasicAttackEngage{TargetGid: enterworld.ObjectIDForCharacter(v)}.Encode())
	if enterworld.CharacterAlive(v) {
		t.Fatal("victim survived a one-HP hit")
	}
	if a.PK == nil || a.PK.TotalCount != 1 || a.PK.Penalty != 1200 || a.PK.DailyCount != 1 {
		t.Fatalf("killer record %+v, want total 1, penalty 1200, daily 1", a.PK)
	}
	if a.PVPState() != 2 {
		t.Fatalf("killer PvP state %d, want murderer 2", a.PVPState())
	}
	if granted != 26 {
		t.Fatalf("PvP EXP %d, want 26", granted)
	}
}

/*
================
TestMurdererVictimDoublesPvpExperience
================
*/
func TestMurdererVictimDoublesPvpExperience(t *testing.T) {
	rt, _, a, v := newPvpPair(t)
	v.CurrentHP = testInt64(1)
	v.PK = &domain.PKRecord{Penalty: 500}
	var granted int64
	rt.UpdateExperience = func(c *enterworld.Character, exp, _ int64, _ uint32) ([]wire.Frame, bool) {
		if c == a {
			granted += exp
		}
		return nil, true
	}
	rt.HandleTargetInteract(testDivision, a, wire.BasicAttackEngage{TargetGid: enterworld.ObjectIDForCharacter(v)}.Encode())
	if granted != 52 {
		t.Fatalf("PvP EXP %d, want doubled 52", granted)
	}
}

/*
================
TestJobKillExperienceRatio

4103E0: the victim's basis against the killer's, held to 0.5..1.5, halved
for a trader killer.
================
*/
func TestJobKillExperienceRatio(t *testing.T) {
	levels := jobKillLevels{gold: map[int64]int64{1: 80, 10: 800}}
	low, high := &enterworld.Character{Level: testInt64(1)}, &enterworld.Character{Level: testInt64(10)}
	// Victim basis trunc(800*10*0.125) = 1000 against 100: held at 1.5.
	if got := jobKillExperience(levels, low, 10); got != 1500 {
		t.Fatalf("high victim = %d, want 1500", got)
	}
	// Victim basis 100 against 1000: held at 0.5.
	if got := jobKillExperience(levels, high, 1); got != 50 {
		t.Fatalf("low victim = %d, want 50", got)
	}
}

/*
================
TestPlayerKnockdownHoldsMotion

A knockdown on a player holds motion 8: its ground commands are locked
until KORecover + action duration + 0.5 s.
================
*/
func TestPlayerKnockdownHoldsMotion(t *testing.T) {
	rt, clock, a, v := newPvpPair(t)
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	skill := enterworld.SkillRow{ID: 9, ActionDurationMs: 1000,
		Knockdown: enterworld.SkillKnockdown{Present: true, Rank: 10, Chance: 100}}
	now := clock.NowMs()
	from := rt.liveSpawn(simulation.WorldKey(testDivision, a.Name), a, now)
	at := rt.liveSpawn(simulation.WorldKey(testDivision, v.Name), v, now)
	d, err := rt.planPlayerDisplacement(displacementRoll{division: testDivision,
		actor: criticalActor{division: testDivision, character: a.Name}, from: from, skill: skill, victim: v, at: at, now: now})
	if err != nil || d == nil || !d.down {
		t.Fatalf("knockdown plan %+v, %v", d, err)
	}
	if _, _, ok := rt.commitPlayerDisplacementInDoor(testDivision, v, d); !ok {
		t.Fatal("knockdown did not commit")
	}
	if !rt.PlayerKnockedDown(testDivision, v.Name, now) {
		t.Fatal("victim is not down")
	}
	// 3000 ms recovery + 1000 ms action + 500 ms.
	if rt.PlayerKnockedDown(testDivision, v.Name, now+4500) {
		t.Fatal("victim still down after its hold")
	}
}

/*
================
TestAreaCandidatesIncludeHostilePlayers
================
*/
func TestAreaCandidatesIncludeHostilePlayers(t *testing.T) {
	rt, clock, a, v := newPvpPair(t)
	rt.RewardActorPresent = func(string, string) bool { return true }
	now := clock.NowMs()
	skill := enterworld.SkillRow{Attack: enterworld.SkillAttack{Present: true}}
	q := areaQuery{division: testDivision, caster: a, skill: skill, center: rt.liveSpawn(simulation.WorldKey(testDivision, a.Name), a, now), reach: 50, now: now}
	found := false
	for _, candidate := range rt.areaPlayerCandidates(q) {
		found = found || candidate.target.player == v
	}
	if !found {
		t.Fatal("the hostile player is not an area candidate")
	}
}

/*
================
jobKillLevels

A level source with only the dg.txt gold basis the job formulas read.
================
*/
type jobKillLevels struct {
	enterworld.LevelDataSource
	gold map[int64]int64
}

/*
================
WithdrawalGoldBasis
================
*/
func (l jobKillLevels) WithdrawalGoldBasis(level int64) (int64, bool) {
	value, ok := l.gold[level]
	return value, ok
}

/*
================
TestThiefMonsterKillPaysHunterJobExp

4E1F60's monster branch: a hunter's killing blow on a thief monster earns
job EXP; a thief monster killed outside job mode earns none.
================
*/
func TestThiefMonsterKillPaysHunterJobExp(t *testing.T) {
	rt, _, c, mob := newCombatTestRuntime(t, 100)
	mob.Ref.TidWord, mob.Ref.TypeID4 = 0x00c6, 2 // 1/2/1/2: a thief monster
	var paid int64
	rt.UpdateJobExperience = func(member *enterworld.Character, delta int64) ([]wire.Frame, bool) {
		if member == c {
			paid += delta
		}
		return nil, true
	}
	if frames, _ := rt.payMonsterJobKillInDoor(testDivision, c, mob, monster.Pose{}, 0); frames != nil || paid != 0 {
		t.Fatal("a kill outside job mode paid job EXP")
	}
	suit := &enterworld.ItemRef{RefObjID: 13, Codename: "ITEM_CH_HUNTER_SUIT", TypeIDs: [4]int64{3, 1, 7, 3}}
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: int64(enterworld.JobSuitSlot),
		RefObjID: suit.RefObjID, Codename: suit.Codename, TypeFlags: suit.TypeFlags(), VarianceBits: "0", StackCount: 1})
	rt.deps.(*enterworld.Deps).Levels = jobKillLevels{LevelDataSource: rt.deps.LevelData(), gold: map[int64]int64{1: 80}}
	rt.payMonsterJobKillInDoor(testDivision, c, mob, monster.Pose{}, 0)
	// An even level: basis trunc(80 * 10 * 0.125) = 100 at ratio 1.
	if paid != 100 {
		t.Fatalf("job EXP %d, want 100", paid)
	}
}
