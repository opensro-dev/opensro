/*
===========================================================================

timeddefense_test.go - equipment-bound and targeted defense buffs

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/testsupport/gamedatatest"
	"testing"
	"time"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

const (
	swordShieldA1  = 27   // SKILL_CH_SWORD_SHIELD_A_01: dura 15000, reqi 4 1 (a Chinese shield), defp 48 0 0
	guardIntercept = 7260 // SKILL_EU_WARRIOR_GUARDA_INTERCEPT_A_01: ally within 150, defp 541 0 0
)

// shieldFixture is the combat fixture with SWORD_SHIELD learned as shipped
// (its reqi kept), the shipped Chinese shield known, and its monster.
func shieldFixture(t *testing.T) (*Runtime, *fakeClock, *enterworld.Character, *enterworld.ItemRef, monster.Instance) {
	t.Helper()
	dir := gamedatatest.TextdataDir(t)
	rt, clock, c, mob := newCombatTestRuntime(t, 100000)
	c.BattleUntilMs = 0
	row, ok := shippedSkills(t).SkillByID(swordShieldA1)
	if !ok || !row.TimedEffect.Pinned || row.TimedEffect.Targeted || !row.Reqi.Present {
		t.Fatalf("shipped SWORD_SHIELD row %+v", row.TimedEffect)
	}
	rt.deps.SkillData().(staticSkillSource)[swordShieldA1] = row
	c.Skills = append(c.Skills, swordShieldA1)
	mp := int64(100)
	c.CurrentMP = &mp
	shield, ok := enterworld.NewTextdataItems(dir).ItemRefByCodename("ITEM_CH_SHIELD_01_A")
	if !ok {
		t.Fatal("shipped shield missing")
	}
	rt.deps.ItemReferences().(staticItemSource)[shield.Codename] = shield
	return rt, clock, c, shield, mob
}

/*
==================
TestSwordShieldNeedsAndKeepsItsShield

58D480 refuses the cast without a shield; with one the buff installs and
raises physical defense; 50F1F0 -> 59F0E0 ends it the moment the shield
leaves slot 7.
==================
*/
func TestSwordShieldNeedsAndKeepsItsShield(t *testing.T) {
	rt, clock, c, shield, _ := shieldFixture(t)
	if r := castSelf(rt, c, swordShieldA1); hasSkillEffect(rt, c.Name, swordShieldA1) || len(r.Frames) == 0 || r.Frames[0].Payload[0] != 2 {
		t.Fatalf("cast without a shield: %+v", r)
	}

	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
		Slot: 7, RefObjID: shield.RefObjID, Codename: shield.Codename, TypeFlags: shield.TypeFlags(), VarianceBits: "0", Durability: 1, StackCount: 1,
	})
	before, err := rt.PlayerBaseStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	if r := castSelf(rt, c, swordShieldA1); !hasSkillEffect(rt, c.Name, swordShieldA1) {
		t.Fatalf("cast with a shield refused: %+v", r)
	}
	buffed, _ := rt.PlayerBaseStats(testDivision, c)
	if buffed.PhysicalDefense <= before.PhysicalDefense {
		t.Fatalf("physical defense %d, was %d", buffed.PhysicalDefense, before.PhysicalDefense)
	}

	clock.Advance(time.Second)
	rt.drainSkillFinalizes(clock.NowMs())
	move := rt.HandleItemMove(testDivision, c, encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: 7, DestSlot: 20, Quantity: 1}))
	if len(move.Frames) == 0 || move.Frames[0].Payload[0] != 1 {
		t.Fatalf("unequip refused: %+v", move.Frames)
	}
	if hasSkillEffect(rt, c.Name, swordShieldA1) {
		t.Fatal("the buff outlived its shield")
	}
}

/*
==================
TestGuardInterceptLandsOnTheAlly

The Warrior's guard is paid by the caster and installed on the allied
player (context mode 2), not on the caster.
==================
*/
func TestGuardInterceptLandsOnTheAlly(t *testing.T) {
	rt, _, c := concealmentFixture(t, guardIntercept)
	ally := nearbyCharacter(rt, c, 21, "ally", 1)
	mp := *c.CurrentMP
	r := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: guardIntercept, HasTarget: true, TargetGid: enterworld.ObjectIDForCharacter(ally)}.Encode())
	if r.DiagnosticRefusal != "" || !hasSkillEffect(rt, ally.Name, guardIntercept) || hasSkillEffect(rt, c.Name, guardIntercept) {
		t.Fatalf("guard: %+v; ally %v caster %v", r, hasSkillEffect(rt, ally.Name, guardIntercept), hasSkillEffect(rt, c.Name, guardIntercept))
	}
	for _, e := range rt.effects.Snapshot(testDivision, ally.Name) {
		if e.SkillID == guardIntercept && e.Phase != 2 {
			t.Fatalf("ally instance phase %d, want 2", e.Phase)
		}
	}
	if *c.CurrentMP >= mp {
		t.Fatal("the caster did not pay")
	}
}

const mountainShieldB1 = 28 // SKILL_CH_SWORD_SHIELD_B_01: dura 15000, reqi 4 1, br 7 10

/*
==================
TestMountainShieldRaisesBlockAndBlocksAHit

br 7 10 adds 10 to the physical block-rate bonuses 0x88 / 0x89 (594AC0), so
a physical basic hit's block chance rises by 10 over the shield's own rate
(58E73C). A blocked monster hit is a bare type-2 record and leaves the
player's HP unchanged (0x58F0EF -> 0x5905FB).
==================
*/
func TestMountainShieldRaisesBlockAndBlocksAHit(t *testing.T) {
	rt, clock, c, shield, mob := shieldFixture(t)
	row, ok := shippedSkills(t).SkillByID(mountainShieldB1)
	if !ok || !row.TimedEffect.Pinned || !row.TimedEffect.Block.Present || row.TimedEffect.Block.Mask&7 != 7 {
		t.Fatalf("shipped Mountain Shield row %+v", row.TimedEffect)
	}
	rt.deps.SkillData().(staticSkillSource)[mountainShieldB1] = row
	c.Skills = append(c.Skills, mountainShieldB1)
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
		Slot: 7, RefObjID: shield.RefObjID, Codename: shield.Codename, TypeFlags: shield.TypeFlags(), VarianceBits: "0", Durability: 1, StackCount: 1,
	})
	before, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	if r := castSelf(rt, c, mountainShieldB1); !hasSkillEffect(rt, c.Name, mountainShieldB1) {
		t.Fatalf("Mountain Shield refused: %+v", r)
	}
	after, _, _ := rt.playerCombatStats(testDivision, c)
	for _, id := range []uint16{0x88, 0x89} {
		if v, _ := after.Param(id); v != float32(row.TimedEffect.Block.Value) {
			t.Fatalf("param %#x = %v, want %d", id, v, row.TimedEffect.Block.Value)
		}
	}
	var monsterAttacker combat.Stats
	if got, base := combat.BlockChance(monsterAttacker, after, 5, false), combat.BlockChance(monsterAttacker, before, 5, false); got != base+10 {
		t.Fatalf("block chance %d, want %d + 10", got, base)
	}

	clock.Advance(2 * time.Second)
	rt.drainSkillFinalizes(clock.NowMs())
	mob.Ref.DefaultSkillIDs[0] = 2
	skills := rt.deps.SkillData().(staticSkillSource)
	attack := skills[2]
	attack.Attack.Min, attack.Attack.Max, attack.Attack.Percent, attack.Attack.Flags, attack.Attack.ImpactCount = 60, 60, 100, 5, 1
	skills[2] = attack
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	hp := enterworld.CurrentHP(c)
	hit := rt.MonsterBasicAttack(testDivision, mob, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
	if !hit.Accepted {
		t.Fatalf("monster hit refused: %+v", hit)
	}
	p := hit.Frames[0].Payload
	if p[19] != 1 || p[20] != 1 || p[25] != 2 || len(p) != 26 {
		t.Fatalf("blocked record % X", p)
	}
	if enterworld.CurrentHP(c) != hp {
		t.Fatalf("HP %d -> %d through a block", hp, enterworld.CurrentHP(c))
	}
}
