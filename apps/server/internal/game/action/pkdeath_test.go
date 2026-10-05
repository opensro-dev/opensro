/*
===========================================================================

pkdeath_test.go - a player's death costs what 4E6EC0 says

A murderer killed by a monster is relieved (-600, daily -1); a penalty of
30000 or more always drops an item; a job wearer killed by an opposing job
monster dies a job death; a monster kill eases a murderer's penalty.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
killPlayer

The fixture monster strikes c for 100 with c at 1 HP.
================
*/
func killPlayer(t *testing.T, rt *Runtime, clock *fakeClock, c *enterworld.Character, mob monster.Instance) simulation.MonsterAttackResult {
	t.Helper()
	mob.Ref.DefaultSkillIDs[0] = 2
	c.CurrentHP = testInt64(1)
	c.Level = testInt64(11)
	c.MaxLevel = testInt64(11)
	c.BattleUntilMs = 0
	skills := rt.deps.SkillData().(staticSkillSource)
	strike := skills[2]
	strike.Attack.Min, strike.Attack.Max, strike.Attack.Percent = 100, 100, 100
	skills[2] = strike
	result := rt.MonsterBasicAttack(testDivision, mob, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
	if !result.Accepted || result.TargetAlive {
		t.Fatalf("fatal strike = accepted %v alive %v", result.Accepted, result.TargetAlive)
	}
	return result
}

/*
================
hasSimOpcode
================
*/
func hasSimOpcode(frames []simulation.Frame, opcode uint16) bool {
	for _, f := range frames {
		if f.Opcode == opcode {
			return true
		}
	}
	return false
}

/*
================
TestMurdererKilledByMonsterIsRelieved
================
*/
func TestMurdererKilledByMonsterIsRelieved(t *testing.T) {
	rt, clock, c, mob := newCombatTestRuntime(t, 100)
	c.PK = &domain.PKRecord{Penalty: 500, DailyCount: 2, DailyDay: 20990101}
	result := killPlayer(t, rt, clock, c, mob)
	if c.PK.Penalty != 0 || c.PK.DailyCount != 1 {
		t.Fatalf("record = %+v, want penalty 0 and daily 1", c.PK)
	}
	if !hasSimOpcode(result.TargetFrames, wire.OpPKPenalty) || !hasSimOpcode(result.TargetFrames, wire.OpPKDaily) {
		t.Fatalf("victim frames lack the PK updates: %+v", result.TargetFrames)
	}
	if c.PVPState() != 0 {
		t.Fatalf("PvP state = %d, want 0 once the penalty is gone", c.PVPState())
	}
}

/*
================
TestHeavyPenaltyAlwaysDrops

Penalty 30000: chance 100. The fixture's rand() is 100: the equipped roll
lands on the empty slot 9, so the only bag item goes.
================
*/
func TestHeavyPenaltyAlwaysDrops(t *testing.T) {
	rt, clock, c, mob := newCombatTestRuntime(t, 100)
	potion := &enterworld.ItemRef{RefObjID: 9, Codename: "ITEM_ETC_HP_POTION_01", TypeIDs: [4]int64{3, 3, 1, 1}, CanDropOnDeath: true}
	rt.deps.ItemReferences().(staticItemSource)[potion.Codename] = potion
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 13, RefObjID: potion.RefObjID,
		Codename: potion.Codename, TypeFlags: potion.TypeFlags(), VarianceBits: "0", StackCount: 5})
	c.PK = &domain.PKRecord{Penalty: 30000}
	result := killPlayer(t, rt, clock, c, mob)
	for _, row := range c.MissionInventory {
		if row.Slot == 13 {
			t.Fatal("the bag item is still carried")
		}
	}
	if !hasSimOpcode(result.Frames, wire.OpSingleObjectSpawn) || !hasSimOpcode(result.TargetFrames, wire.OpItemMoveResponse) {
		t.Fatalf("no drop frames: public %+v private %+v", result.Frames, result.TargetFrames)
	}
	if c.PK.Penalty != 30000-600 {
		t.Fatalf("penalty = %d, want 29400", c.PK.Penalty)
	}
}

/*
================
TestUndroppableItemsStay

An event item (TID 3/3/9), a cash item and a CanDrop 1 item never drop.
================
*/
func TestUndroppableItemsStay(t *testing.T) {
	for _, ref := range []*enterworld.ItemRef{
		{RefObjID: 9, Codename: "ITEM_ETC_E_EVENT", TypeIDs: [4]int64{3, 3, 9, 0}, CanDropOnDeath: true},
		{RefObjID: 10, Codename: "ITEM_MALL_ANY", TypeIDs: [4]int64{3, 3, 1, 1}, CanDropOnDeath: true, CashItem: true},
		{RefObjID: 11, Codename: "ITEM_ETC_DETECT_01", TypeIDs: [4]int64{3, 3, 1, 1}},
	} {
		rt, clock, c, mob := newCombatTestRuntime(t, 100)
		rt.deps.ItemReferences().(staticItemSource)[ref.Codename] = ref
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 13, RefObjID: ref.RefObjID,
			Codename: ref.Codename, TypeFlags: ref.TypeFlags(), VarianceBits: "0", StackCount: 1})
		c.PK = &domain.PKRecord{Penalty: 30000}
		killPlayer(t, rt, clock, c, mob)
		if len(c.MissionInventory) != 2 {
			t.Fatalf("%s dropped", ref.Codename)
		}
	}
}

/*
================
TestJobWearerKilledByThiefIsAJobDeath
================
*/
func TestJobWearerKilledByThiefIsAJobDeath(t *testing.T) {
	rt, _, c, mob := newCombatTestRuntime(t, 100)
	suit := &enterworld.ItemRef{RefObjID: 12, Codename: "ITEM_CH_TRADE_SUIT", TypeIDs: [4]int64{3, 1, 7, 1}}
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: int64(enterworld.JobSuitSlot),
		RefObjID: suit.RefObjID, Codename: suit.Codename, TypeFlags: suit.TypeFlags(), VarianceBits: "0", StackCount: 1})
	mob.Ref.TidWord, mob.Ref.TypeID4 = 0x00c6, 2 // 1/2/1/2: a thief monster
	if !mob.ThiefMonster() {
		t.Fatal("fixture monster is not a thief")
	}
	if kind := rt.deathKind(testDivision, c, deathKiller{monster: &mob}); kind != pk.DeathJob {
		t.Fatalf("kind = %d, want a job death", kind)
	}
	mob.Ref.TypeID4 = 1
	if kind := rt.deathKind(testDivision, c, deathKiller{monster: &mob}); kind != pk.DeathMonster {
		t.Fatalf("kind = %d, want a monster death", kind)
	}
	if kind := rt.deathKind(testDivision, c, deathKiller{}); kind != pk.DeathNone {
		t.Fatalf("no killer kind = %d", kind)
	}
}

/*
================
TestMonsterKillEasesThePenalty

4C42F0 -> 4EB6B0: a monster four levels above its killer eases 10.
================
*/
func TestMonsterKillEasesThePenalty(t *testing.T) {
	rt, clock, c, mob := newCombatTestRuntimeAtLevel(t, 1, 5)
	c.PK = &domain.PKRecord{Penalty: 100}
	row := rt.deps.SkillData().(staticSkillSource)[2]
	snapshot := rt.characterSnapshot(testDivision, c)
	hit, ok := rt.commitCreditedMonsterHit(testDivision, c, snapshot, row, mob, combat.Result{Damage: 50, ResultFlags: 1}, "test-kill", clock.NowMs())
	if !ok || !hit.impacts[0].Fatal {
		t.Fatal("the kill did not commit")
	}
	if c.PK.Penalty != 90 {
		t.Fatalf("penalty = %d after the kill, want 90", c.PK.Penalty)
	}
	if !hasOpcode(hit.settlement.actorFrames, wire.OpPKPenalty) {
		t.Fatal("no 0x30F2 for the killer")
	}
}
