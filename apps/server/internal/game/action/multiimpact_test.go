/*
===========================================================================

multiimpact_test.go - multi-hit areas, multi-arrow shots and HP costs

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
==================
TestAreaCarriesEveryImpactPerVictim

58E5F0 loops every mc impact over every target group: each victim gets one
record per impact and loses their sum. A victim killed by an earlier
impact gets bare type-8 records for the rest (58EE1B).
==================
*/
func TestAreaCarriesEveryImpactPerVictim(t *testing.T) {
	for _, fatal := range []bool{false, true} {
		hp := uint32(100000)
		if fatal {
			hp = 1
		}
		rt, targets := areaFixture(t, hp)
		c := rt.findCharacter(testDivision, "asd2")
		skill := shippedOffense(t, "SKILL_CH_LIGHTNING_CHUNDUNG_A_01")
		skill.Attack.ImpactCount = 3
		rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
		c.Skills = append(c.Skills, skill.ID)
		c.Intellect = testInt64(200)
		c.CurrentMP = testInt64(1000)
		result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: targets[0].Gid}.Encode())
		if len(result.Frames) == 0 || result.Frames[0].Opcode != wire.OpSkillCastResult {
			t.Fatalf("area refused %+v", result)
		}
		p := result.Frames[0].Payload
		if p[19] != 3 || p[20] != 3 {
			t.Fatalf("matrix % X", p)
		}
		at := 21
		for victim := range 3 {
			gid := binary.LittleEndian.Uint32(p[at:])
			at += 4
			var sum uint32
			for impact := range 3 {
				tag := p[at]
				switch {
				case fatal && impact == 0:
					if tag != 0x80 {
						t.Fatalf("victim %d: first record tag %#x, want fatal", victim, tag)
					}
				case fatal:
					if tag != 8 {
						t.Fatalf("victim %d impact %d: tag %#x, want 8 after the kill", victim, impact, tag)
					}
					at++
					continue
				case tag != 0:
					t.Fatalf("victim %d impact %d: tag %#x", victim, impact, tag)
				}
				sum += binary.LittleEndian.Uint32(p[at+1:]) >> 8
				at += 9
			}
			after, _ := rt.Monsters.Get(testDivision, gid)
			if hp-after.CurrentHP != min(hp, sum) {
				t.Fatalf("victim %d lost %d, records sum %d", victim, hp-after.CurrentHP, sum)
			}
		}
		if at != len(p) {
			t.Fatalf("trailing bytes: at %d of %d", at, len(p))
		}
	}
}

// bowChainFixture arms the combat fixture with a bow and a stack of arrows.
/*
================
bowChainFixture
================
*/
func bowChainFixture(t *testing.T, arrows int64) (*Runtime, *enterworld.Character, enterworld.SkillRow, uint32) {
	t.Helper()
	rt, targets := areaFixture(t, 100000)
	c := rt.findCharacter(testDivision, "asd2")
	items := rt.deps.ItemReferences().(staticItemSource)
	bow := *items[c.MissionInventory[0].Codename]
	bow.Codename, bow.RefObjID, bow.TypeIDs[3] = "ITEM_CH_BOW_01_A", 74, 6
	combatRef := *bow.Combat
	combatRef.ActionRange = 180
	bow.Combat = &combatRef
	items[bow.Codename] = &bow
	c.MissionInventory[0].Codename, c.MissionInventory[0].RefObjID, c.MissionInventory[0].TypeFlags = bow.Codename, bow.RefObjID, bow.TypeFlags()
	arrow := &enterworld.ItemRef{RefObjID: 62001, Codename: "ITEM_ETC_AMMO_ARROW_01", TypeIDs: [4]int64{3, 3, 4, 1}}
	items[arrow.Codename] = arrow
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 7, RefObjID: arrow.RefObjID, Codename: arrow.Codename, TypeFlags: arrow.TypeFlags(), StackCount: arrows})

	skill := shippedOffense(t, "SKILL_CH_BOW_CHAIN_A_01")
	if !skill.DirectOffensePinned || skill.Attack.ImpactCount != 2 || skill.Ammunition.Count != 1 {
		t.Fatalf("BOW_CHAIN_A not admitted: impacts %d ammo %+v refusal %q", skill.Attack.ImpactCount, skill.Ammunition, skill.OffenseRefusal)
	}
	skill.Consumption.MP = 10
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.CurrentMP = testInt64(150)
	return rt, c, skill, targets[0].Gid
}

/*
================
arrowStack
================
*/
func arrowStack(c *enterworld.Character) (int64, bool) {
	for _, row := range c.MissionInventory {
		if row.Slot == 7 {
			return row.StackCount, true
		}
	}
	return 0, false
}

/*
==================
TestBowChainSpendsAnArrowPerImpact

585AF0: a shot spends cnsm count x mc impacts arrows (BOW_CHAIN_A: 1 x 2)
and both impacts land at release. With a single arrow left the shot is
still admitted (58E348 tests only the item) and spends that one.
==================
*/
func TestBowChainSpendsAnArrowPerImpact(t *testing.T) {
	for _, tc := range []struct {
		arrows, left int64
	}{{5, 3}, {1, 0}} {
		rt, c, skill, target := bowChainFixture(t, tc.arrows)
		start := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target}.Encode())
		if len(start.Frames) != 1 || start.Frames[0].Payload[0] != 1 {
			t.Fatalf("%d arrows: cast refused %+v", tc.arrows, start)
		}
		result := releasePreparedSkillForTest(t, rt, rt.Now().UnixMilli()+int64(skill.ActionCastingTimeMs)+1)
		p := result.Frames[0].Payload
		if p[10] != 2 || p[11] != 1 {
			t.Fatalf("%d arrows: release % X", tc.arrows, p)
		}
		left, present := arrowStack(c)
		if left != tc.left || present != (tc.left != 0) {
			t.Fatalf("%d arrows: %d left (present %v), want %d", tc.arrows, left, present, tc.left)
		}
	}
}

/*
==================
TestHPCostChecksMaximumChargesCurrentAndNeverKills

58E1B6: current HP below the flat + percent-of-MAXIMUM cost refuses 0x3013.
58312C charges the percent of CURRENT HP; 4A8770 clamps the debit so the
caster keeps 1 HP.
==================
*/
func TestHPCostChecksMaximumChargesCurrentAndNeverKills(t *testing.T) {
	skill := enterworld.SkillRow{Consumption: enterworld.SkillConsumption{HPPercent: 10, Pinned: true}}
	if hpCostRefusal(9, 100, skill) != 0x3013 || hpCostRefusal(10, 100, skill) != 0 {
		t.Fatal("HP admission boundary")
	}

	rt, _, c, _ := newCombatTestRuntime(t, 100000)
	maxHP, _, _, _ := rt.playerKeeperVitals(testDivision, c)
	c.CurrentHP = testInt64(maxHP / 2)
	if got := rt.preparedExecutionHPCost(testDivision, c, skill); got != int64(crtFtol(float64(int32(maxHP/2))*10/100)) {
		t.Fatalf("prepared HP cost %d from current %d", got, maxHP/2)
	}
	rt.commitOffensiveResources(testDivision, c, skillCharge{hp: 10})
	if got := enterworld.CurrentHP(c); got != maxHP/2-10 {
		t.Fatalf("HP %d after a 10 HP charge, want %d", got, maxHP/2-10)
	}
	rt.commitOffensiveResources(testDivision, c, skillCharge{hp: maxHP})
	if got := enterworld.CurrentHP(c); got != 1 {
		t.Fatalf("HP %d after an overcharge, want 1", got)
	}
}

/*
==================
TestWhirlwindPaysItsHPCost

SKILL_EU_WARRIOR_DUALA_WHIRLWIND_A_01 (3 impacts, efr shape 2, 10 % HP):
the shipped row is admitted, strikes every nearby victim three times and
charges 10 % of the caster's current HP.
==================
*/
func TestWhirlwindPaysItsHPCost(t *testing.T) {
	rt, targets := areaFixture(t, 100000)
	c := rt.findCharacter(testDivision, "asd2")
	skill := shippedOffense(t, "SKILL_EU_WARRIOR_DUALA_WHIRLWIND_A_01")
	if !skill.OffensiveStagePinned || skill.Consumption.HPPercent != 10 || skill.Attack.ImpactCount != 3 || skill.OffensiveArea.Radius == 0 {
		t.Fatalf("WHIRLWIND not admitted: %q %+v", skill.OffenseRefusal, skill.Consumption)
	}
	skill.RequiredWeaponKinds = [2]uint8{0xff, 0xff}
	skill.Reqi = enterworld.SkillReqi{}
	skill.Consumption.MP = 10
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	stage := shippedOffense(t, "SKILL_EU_WARRIOR_DUALA_WHIRLWIND_A2_01") // the combo's linked stage
	stage.RequiredWeaponKinds = [2]uint8{0xff, 0xff}
	rt.deps.SkillData().(staticSkillSource)[stage.ID] = stage
	c.Skills = append(c.Skills, skill.ID)
	c.CurrentMP = testInt64(150)
	hp := enterworld.CurrentHP(c)
	want := hp - int64(crtFtol(float64(int32(hp))*10/100))

	start := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: targets[0].Gid}.Encode())
	if len(start.Frames) == 0 || start.Frames[0].Opcode != wire.OpSkillCastResult || start.Frames[0].Payload[0] != 1 {
		t.Fatalf("whirlwind refused: %+v", start)
	}
	result := releasePreparedSkillForTest(t, rt, rt.Now().UnixMilli()+int64(skill.ActionCastingTimeMs)+1)
	p := result.Frames[0].Payload
	// Release: [1][token][gid][flags][impacts][targets]...
	if p[0] != 1 || p[10] != 3 || p[11] == 0 {
		t.Fatalf("whirlwind release % X", p)
	}
	if got := enterworld.CurrentHP(c); got != want {
		t.Fatalf("caster HP %d, want %d (10 %% of %d)", got, want, hp)
	}
}
