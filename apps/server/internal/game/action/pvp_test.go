/*
===========================================================================

pvp_test.go - players striking players, and what a kill costs and pays

The pair stands in a PvP region (0x62AA) beside each other; the attacker
already holds the victim as an aggressor, which the native target validation admits before its
level gates, so the level-1 fixture rows stay valid.

===========================================================================
*/

package action

import (
	"math"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
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

Natively party protection precedes every other relation.
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

The native kill rewards, kind 3: total +1, penalty (total+1)/2*1200, daily +1 at an even
level, and the native PvP EXP (two level-1 players: 24 * 1.09 = 26).
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

The native job kill formula: the victim's basis against the killer's, held to 0.5..1.5, halved
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
	q := areaQuery{selects: 24, division: testDivision, caster: a, skill: skill, center: rt.liveSpawn(simulation.WorldKey(testDivision, a.Name), a, now), reach: 50, now: now}
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
TestAreaCandidatesRefuseWithoutCopyingTheRefused

A player the attack validation refuses on its own record facts alone
(one aggression weight) is weighed by the field-only pre-filter under
the door (#1943): no candidate, and the weighing must not pay for a
detached copy of the refused player. The same pair with no such fact
is copied and judged outside (TestAreaCandidatesIncludeHostilePlayers).
================
*/
func TestAreaCandidatesRefuseWithoutCopyingTheRefused(t *testing.T) {
	rt, clock, a, v := newPvpPair(t)
	a.Aggressions = nil
	v.Aggressions = map[uint32]uint32{enterworld.ObjectIDForCharacter(a): 1}
	// A carried bag of production shape, so one detached copy costs
	// what the live shard pays for it; plain stack rows move no
	// verdict the walk reads.
	for slot := int64(20); slot < 60; slot++ {
		v.MissionInventory = append(v.MissionInventory, enterworld.InventoryRow{
			Slot: slot, RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", StackCount: 1, MagicOptions: []uint64{1}})
	}
	rt.RewardActorPresent = func(string, string) bool { return true }
	now := clock.NowMs()
	skill := enterworld.SkillRow{Attack: enterworld.SkillAttack{Present: true}}
	q := areaQuery{selects: 24, division: testDivision, caster: a, skill: skill, center: rt.liveSpawn(simulation.WorldKey(testDivision, a.Name), a, now), reach: 50, now: now}
	if got := rt.areaPlayerCandidates(q); len(got) != 0 {
		t.Fatalf("the pre-filtered player became an area candidate: %+v", got)
	}
	copyAllocs := testing.AllocsPerRun(20, func() { rt.characterSnapshot(testDivision, v) })
	candidateAllocs := testing.AllocsPerRun(20, func() { rt.areaPlayerCandidates(q) })
	t.Logf("allocs: one character copy %.0f, refused candidate walk %.0f", copyAllocs, candidateAllocs)
	if candidateAllocs >= copyAllocs {
		t.Fatalf("refused players still pay for their copy: %.0f allocs; one copy is %.0f",
			candidateAllocs, copyAllocs)
	}
}

/*
================
TestAreaPlayerPreRefusalOnlyRefusesWhatTheValidationRefuses

Every refusal the field-only pre-filter gives under the door is one
the full attack validation gives on the same pair, so skipping the
copy cannot move a verdict. The firing cases keep the pair's admitting
relation armed (the caster's aggression, which the validation admits
on later in its order than the pre-filter's facts), and the control
cases pin the neighbouring facts that must not refuse.
================
*/
func TestAreaPlayerPreRefusalOnlyRefusesWhatTheValidationRefuses(t *testing.T) {
	attack := enterworld.SkillAttack{Present: true}
	firing := []struct {
		name  string
		skill enterworld.SkillRow
		arm   func(a, v *enterworld.Character)
	}{
		{name: "building row",
			skill: enterworld.SkillRow{Attack: attack,
				Targets: enterworld.SkillTargets{Present: true, Building: true}}},
		{name: "land row",
			skill: enterworld.SkillRow{Attack: attack,
				Targets: enterworld.SkillTargets{Present: true, Land: true}}},
		{name: "dead-body row on a living player",
			skill: enterworld.SkillRow{Attack: attack,
				Targets:     enterworld.SkillTargets{DeadBody: true},
				Replacement: statuseffect.ReplacementDescriptor{MatchesExecutionSelector: true}}},
		{name: "monster-only enemy row",
			skill: enterworld.SkillRow{Attack: attack,
				Targets: enterworld.SkillTargets{EnemyM: true}}},
		{name: "resurrection level cap",
			skill: enterworld.SkillRow{Attack: attack,
				Abnormal: abnormal.SkillParams{AdmitDeadParty: true, ResuMaxLevel: 10}},
			arm: func(a, v *enterworld.Character) { v.Level = testInt64(50) }},
		{name: "one aggression weight",
			skill: enterworld.SkillRow{Attack: attack},
			arm: func(a, v *enterworld.Character) {
				v.Aggressions = map[uint32]uint32{enterworld.ObjectIDForCharacter(a): 1}
			}},
		{name: "one aggression weight, selector row",
			skill: enterworld.SkillRow{Attack: attack,
				Replacement: statuseffect.ReplacementDescriptor{MatchesExecutionSelector: true}},
			arm: func(a, v *enterworld.Character) {
				v.Aggressions = map[uint32]uint32{enterworld.ObjectIDForCharacter(a): 1}
			}},
	}
	for _, tc := range firing {
		t.Run(tc.name, func(t *testing.T) {
			rt, clock, a, v := newPvpPair(t)
			if tc.arm != nil {
				tc.arm(a, v)
			}
			if !areaPlayerPreRefusal(v, tc.skill) {
				t.Fatal("the pre-filter does not refuse")
			}
			if code := rt.hostilePlayerRefusalWith(testDivision, a, v, tc.skill, clock.NowMs(), playerAttackArea); code == 0 {
				t.Fatal("the pre-filter refuses a pair the full validation admits")
			}
		})
	}
	controls := []struct {
		name  string
		skill enterworld.SkillRow
		arm   func(a, v *enterworld.Character)
	}{
		{name: "plain hostile row",
			skill: enterworld.SkillRow{Attack: attack},
			arm:   func(a, v *enterworld.Character) { v.Aggressions = nil }},
		{name: "two aggression weights",
			skill: enterworld.SkillRow{Attack: attack},
			arm: func(a, v *enterworld.Character) {
				v.Aggressions = map[uint32]uint32{enterworld.ObjectIDForCharacter(a): 2}
			}},
		{name: "animal building row",
			skill: enterworld.SkillRow{Attack: attack,
				Targets: enterworld.SkillTargets{Present: true, Animal: true, Building: true}}},
		{name: "resurrection under the cap",
			skill: enterworld.SkillRow{Attack: attack,
				Abnormal: abnormal.SkillParams{AdmitDeadParty: true, ResuMaxLevel: 10}},
			arm: func(a, v *enterworld.Character) { v.Level = testInt64(5) }},
		// The permission gate admits DontCare before the dead-body and
		// enemy columns, so these rows must reach the full validation.
		{name: "dont-care dead-body row on a living player",
			skill: enterworld.SkillRow{Attack: attack,
				Targets: enterworld.SkillTargets{DontCare: true, DeadBody: true}}},
		{name: "dont-care monster-only enemy row",
			skill: enterworld.SkillRow{Attack: attack,
				Targets: enterworld.SkillTargets{DontCare: true, EnemyM: true}}},
	}
	for _, tc := range controls {
		_, _, a, v := newPvpPair(t)
		if tc.arm != nil {
			tc.arm(a, v)
		}
		if areaPlayerPreRefusal(v, tc.skill) {
			t.Fatalf("%s: the pre-filter refuses", tc.name)
		}
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

The native kill rewards' monster branch: a hunter's killing blow on a thief monster earns
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

/*
================
TestCompanionKillOfAThiefMonsterPaysNoJobExp

The original pays monster job EXP only through a player's own blow: the
same hunter earns it for its own fatal hit and nothing when its pet or
COS lands the blow.
================
*/
func TestCompanionKillOfAThiefMonsterPaysNoJobExp(t *testing.T) {
	rt, clock, c, mob := newCombatTestRuntime(t, 100)
	mob.Ref.TidWord, mob.Ref.TypeID4 = 0x00c6, 2 // 1/2/1/2: a thief monster
	suit := &enterworld.ItemRef{RefObjID: 13, Codename: "ITEM_CH_HUNTER_SUIT", TypeIDs: [4]int64{3, 1, 7, 3}}
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: int64(enterworld.JobSuitSlot),
		RefObjID: suit.RefObjID, Codename: suit.Codename, TypeFlags: suit.TypeFlags(), VarianceBits: "0", StackCount: 1})
	rt.deps.(*enterworld.Deps).Levels = jobKillLevels{LevelDataSource: rt.deps.LevelData(), gold: map[int64]int64{1: 80}}
	var paid int64
	rt.UpdateJobExperience = func(member *enterworld.Character, delta int64) ([]wire.Frame, bool) {
		if member == c {
			paid += delta
		}
		return nil, true
	}
	impact := simulation.MonsterDamageResult{Fatal: true, Instance: mob}
	roster := rt.monsterRewardRoster(testDivision, c, clock.NowMs())
	roster.companionKill = true
	rt.settleMonsterInsideDoor(testDivision, c, roster, impact, monster.Pose{}, clock.NowMs())
	if paid != 0 {
		t.Fatalf("a companion's kill paid %d job EXP, want none", paid)
	}
	roster.companionKill = false
	rt.settleMonsterInsideDoor(testDivision, c, roster, impact, monster.Pose{}, clock.NowMs())
	if paid != 100 {
		t.Fatalf("the hunter's own kill paid %d job EXP, want 100", paid)
	}
}

/*
================
TestPlayerAttackPKLimitsApplyOnlyToNeutrals

Natively a neutral target is refused at the attacker's daily limit
(0x3014); an aggressor target is a legal enemy whose attack
never reads the limits.
================
*/
func TestPlayerAttackPKLimitsApplyOnlyToNeutrals(t *testing.T) {
	rt, clock, a, v := newPvpPair(t)
	level := int64(30)
	a.Level, v.Level = &level, &level
	a.Aggressions = nil
	a.PK = &domain.PKRecord{DailyCount: playerCombatMaxDailyPK}
	if code := rt.playerAttackTargetRefusal(testDivision, a, v, clock.NowMs()); code != 0x3014 {
		t.Fatalf("attack on a neutral at the daily limit = %#x, want 0x3014", code)
	}
	v.Aggressions = map[uint32]uint32{enterworld.ObjectIDForCharacter(a): playerAggressionTicks}
	if code := rt.playerAttackTargetRefusal(testDivision, a, v, clock.NowMs()); code != 0 {
		t.Fatalf("attack on an aggressor at the daily limit = %#x, want admitted", code)
	}
}

/*
================
TestJobAttackIgnoresTheSuitActivation

The native target validation admits opposing jobs before it reads +0x2178, so a suit still
activating fights at once; no 0x3019 WAITFOR_JOB_ACTIVATE is sent.
================
*/
func TestJobAttackIgnoresTheSuitActivation(t *testing.T) {
	rt, clock, a, v := newPvpPair(t)
	items := rt.deps.ItemReferences().(staticItemSource)
	for i, c := range []*enterworld.Character{a, v} {
		suit := &enterworld.ItemRef{RefObjID: uint32(9201 + i), Codename: []string{"ITEM_CH_M_TRADE_TRADER_04", "ITEM_CH_M_TRADE_THIEF_04"}[i],
			Country: 3, TypeIDs: [4]int64{3, 1, 7, int64(1 + i)}, ReqQuadTypes: [4]int64{-1, -1, -1, -1}, Combat: &enterworld.ItemCombatRef{}}
		items[suit.Codename] = suit
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: int64(jobSuitSlot),
			RefObjID: suit.RefObjID, Codename: suit.Codename, TypeFlags: suit.TypeFlags(), StackCount: 1})
	}
	rt.startJobActivation(testDivision, a, clock.NowMs())
	if code := rt.playerAttackTargetRefusal(testDivision, a, v, clock.NowMs()); code != 0 {
		t.Fatalf("job attack while activating = %#x, want admitted", code)
	}
}

/*
================
TestPlayerUnderPKLevelFightsOnlyARetainedOpponent

BR-261006-1818: below level 20 a player is refused (0x3016) on a neutral,
for basic attacks and skills alike (both reach the native target validation), unless the target
is already in its aggression map; then the validation admits before the level
floor, so it may keep fighting that player. The client refuses the click
below 20 (client 693EF9) but sends the skill, which is why only skills landed.
================
*/
func TestPlayerUnderPKLevelFightsOnlyARetainedOpponent(t *testing.T) {
	rt, clock, a, v := newPvpPair(t)
	low, high := int64(15), int64(30)
	a.Level, v.Level = &low, &high
	a.Aggressions = nil
	if code := rt.playerAttackTargetRefusal(testDivision, a, v, clock.NowMs()); code != 0x3016 {
		t.Fatalf("level 15 on a neutral = %#x, want 0x3016", code)
	}
	a.Aggressions = map[uint32]uint32{enterworld.ObjectIDForCharacter(v): playerAggressionTicks}
	if code := rt.playerAttackTargetRefusal(testDivision, a, v, clock.NowMs()); code != 0 {
		t.Fatalf("level 15 on a retained opponent = %#x, want admitted", code)
	}
}

/*
================
TestAttackingAMurdererLeavesTheAttackerWhite

Natively a murderer is a legal opponent, so hitting one records no
aggression and the attacker does not turn purple; hitting a neutral does.
================
*/
func TestAttackingAMurdererLeavesTheAttackerWhite(t *testing.T) {
	rt, clock, a, v := newPvpPair(t)
	level := int64(30)
	a.Level, v.Level = &level, &level
	a.Aggressions = nil
	v.PK = &domain.PKRecord{Penalty: 1000}
	rt.registerPlayerAttack(testDivision, a, v, clock.NowMs())
	if a.PVPState() != 0 || len(a.Aggressions) != 0 {
		t.Fatalf("attacking a murderer left state %d with %d aggressions, want white", a.PVPState(), len(a.Aggressions))
	}
	v.PK = nil
	rt.registerPlayerAttack(testDivision, a, v, clock.NowMs())
	if a.PVPState() != 1 {
		t.Fatalf("attacking a neutral left state %d, want purple", a.PVPState())
	}
}

/*
================
TestAreaSelectChoosesItsKinds

The efr select mask decides which kinds an area may strike: 24 takes the
monster beside the caster, 8 (Mana Drought) hostile characters alone.
================
*/
func TestAreaSelectChoosesItsKinds(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	now := clock.NowMs()
	lease, ok := rt.casterPopulation(testDivision, c)
	if !ok {
		t.Fatal("no caster population")
	}
	skill := enterworld.SkillRow{Attack: enterworld.SkillAttack{Present: true}}
	monsterIn := func(selects uint8) bool {
		q := areaQuery{selects: selects, division: testDivision, caster: c, skill: skill, lease: lease,
			center: rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, now), reach: 500, now: now}
		for _, candidate := range rt.areaCandidates(q) {
			if candidate.target.gid == target.Gid {
				return true
			}
		}
		return false
	}
	if !monsterIn(24) {
		t.Fatal("select 24 left out the monster")
	}
	if monsterIn(8) {
		t.Fatal("select 8 struck a monster")
	}
}

/*
================
TestPlayerKnockbackWalksThroughTheMoveQuery

The native object move moves a pushed player through the move query from its own
cell: a refused walk leaves it where it stood, a clipped walk ends at the
rest, and the published point is where it landed.
================
*/
func TestPlayerKnockbackWalksThroughTheMoveQuery(t *testing.T) {
	for _, tc := range []struct {
		name    string
		refuse  bool
		clipped bool
	}{{"open", false, false}, {"clipped", false, true}, {"refused", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			rt, clock, a, v := newPvpPair(t)
			rt.CombatRoll = func() (uint32, error) { return 0, nil }
			skill := enterworld.SkillRow{ID: 9, ActionDurationMs: 1000,
				Knockback: enterworld.SkillKnockback{Present: true, Chance: 100, Distance: 30}}
			now := clock.NowMs()
			from := rt.liveSpawn(simulation.WorldKey(testDivision, a.Name), a, now)
			at := rt.liveSpawn(simulation.WorldKey(testDivision, v.Name), v, now)
			d, err := rt.planPlayerDisplacement(displacementRoll{division: testDivision,
				actor: criticalActor{division: testDivision, character: a.Name}, from: from, skill: skill, victim: v, at: at, now: now})
			if err != nil || d == nil || d.down {
				t.Fatalf("knockback plan %+v, %v", d, err)
			}
			push := d.pose
			rest := push
			rest.X = (at.X + push.X) / 2
			rt.ConstrainWalk = func(_ string, from simulation.Spawn, _ simulation.NavOwner, to simulation.Spawn) (simulation.Spawn, simulation.NavWalk, *simulation.MoveError) {
				if tc.refuse {
					return from, simulation.NavWalk{}, &simulation.MoveError{Reason: "blocked"}
				}
				if tc.clipped {
					return rest, simulation.NavWalk{}, nil
				}
				return to, simulation.NavWalk{}, nil
			}
			want := push
			switch {
			case tc.refuse:
				want = at
			case tc.clipped:
				want = rest
			}
			point, _, ok := rt.commitPlayerDisplacementInDoor(testDivision, v, d)
			if !ok {
				t.Fatal("knockback did not commit")
			}
			landed := rt.liveSpawn(simulation.WorldKey(testDivision, v.Name), v, now)
			wantPoint, _ := wire.NewSkillCastFacingPoint(want.RegionID, want.X, want.Y, want.Z)
			if math.Abs(landed.X-want.X) > 0.01 || point != wantPoint {
				t.Fatalf("landed at %.2f (published %+v), want %.2f", landed.X, point, want.X)
			}
		})
	}
}

/*
================
TestAreaStrikesSpareNeutralPlayers

The native target validation's control flags, as the hostile-target check passes them for an area victim. Both
players are level 20 in a battle region and the caster is white.

  - A neutral target the caster is not fighting: a direct attack is legal
    (open-field PK, Alt on the client), an area (player or pet) refuses.
  - The same target once the caster fights it (aggression map): legal.
  - A murderer target: an area strikes it.
  - An aggressor target: a player's area spares it (bit 2), a pet's does
    not (mode 1 lacks bit 2).

The area candidate walk applies the same rule: a monster area leaves the
neutral player out.
================
*/
func TestAreaStrikesSpareNeutralPlayers(t *testing.T) {
	rt, clock, a, v := newPvpPair(t)
	now := clock.NowMs()
	level := testInt64(playerCombatMinimumLevel)
	a.Level, v.Level = level, level
	a.Aggressions = nil

	if code := rt.playerAttackRefusal(testDivision, a, v, now, 0); code != 0 {
		t.Fatalf("a direct attack on a neutral player answered %#x", code)
	}
	for _, flags := range []uint8{playerAttackArea, petAttackArea} {
		if code := rt.playerAttackRefusal(testDivision, a, v, now, flags); code == 0 {
			t.Fatalf("an area (flags %d) struck a neutral player", flags)
		}
	}

	a.Aggressions = map[uint32]uint32{enterworld.ObjectIDForCharacter(v): playerAggressionTicks}
	if code := rt.playerAttackRefusal(testDivision, a, v, now, playerAttackArea); code != 0 {
		t.Fatalf("an area spared the player the caster fights: %#x", code)
	}
	a.Aggressions = nil

	v.PK = &domain.PKRecord{Penalty: 1}
	if code := rt.playerAttackRefusal(testDivision, a, v, now, playerAttackArea); code != 0 {
		t.Fatalf("an area spared a murderer: %#x", code)
	}
	v.PK = nil

	v.Aggressions = map[uint32]uint32{999: playerAggressionTicks}
	if v.PVPState() != 1 {
		t.Fatalf("fixture victim state %d, want aggressor", v.PVPState())
	}
	if code := rt.playerAttackRefusal(testDivision, a, v, now, playerAttackArea); code == 0 {
		t.Fatal("a white caster's area struck an aggressor")
	}
	if code := rt.playerAttackRefusal(testDivision, a, v, now, petAttackArea); code != 0 {
		t.Fatalf("a pet's area spared an aggressor: %#x", code)
	}
	v.Aggressions = nil

	rt.RewardActorPresent = func(string, string) bool { return true }
	lease, ok := rt.casterPopulation(testDivision, a)
	if !ok {
		t.Fatal("no caster population")
	}
	// An attack row that may name players (Enemy_M and Enemy_P).
	hostile := enterworld.SkillRow{Attack: enterworld.SkillAttack{Present: true},
		Targets:     enterworld.SkillTargets{Present: true, Animal: true, EnemyM: true, EnemyP: true},
		Replacement: statuseffect.ReplacementDescriptor{MatchesExecutionSelector: true}}
	q := areaQuery{selects: 24, division: testDivision, caster: a, skill: hostile,
		lease: lease, center: rt.liveSpawn(simulation.WorldKey(testDivision, a.Name), a, now), reach: 500, now: now}
	selected := func() bool {
		for _, candidate := range rt.areaCandidates(q) {
			if candidate.target.player != nil && candidate.target.player.ID == v.ID {
				return true
			}
		}
		return false
	}
	a.Aggressions = map[uint32]uint32{enterworld.ObjectIDForCharacter(v): playerAggressionTicks}
	if !selected() {
		t.Fatal("a monster area left out the player the caster fights")
	}
	a.Aggressions = nil
	if selected() {
		t.Fatal("a monster area selected the neutral player standing in it")
	}
}
