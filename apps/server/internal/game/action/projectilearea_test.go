/*
===========================================================================

projectilearea_test.go - area shots in flight: the bow's and the sword's

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
TestBowAreaShotsResolveAtReleaseAndSpendOneArrow

585A1F: a pierce (shape 4, a line) and a special shot (shape 1, around
the caster) start without results, then at release select their victims,
damage them with the authored falloff, charge MP once and spend one arrow.
==================
*/
func TestBowAreaShotsResolveAtReleaseAndSpendOneArrow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		victims int
	}{
		{"SKILL_CH_BOW_PIERCE_A_01", 3},
		{"SKILL_CH_BOW_SPECIAL_A_01", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 7, RefObjID: arrow.RefObjID, Codename: arrow.Codename, TypeFlags: arrow.TypeFlags(), StackCount: 2})

			skill := shippedOffense(t, tc.name)
			if !skill.DirectOffensePinned || skill.ProjectileSpeed == 0 || skill.OffensiveArea.Radius == 0 {
				t.Fatalf("not admitted: %+v", skill)
			}
			rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
			c.Skills = append(c.Skills, skill.ID)
			c.CurrentMP = testInt64(150) // below the level-1 keeper maximum
			rt.CombatRoll = func() (uint32, error) { return 10, nil }

			start := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: targets[0].Gid}.Encode())
			start = assertAndSeparateActionSession(t, start)
			if len(start.Frames) != 1 || start.Frames[0].Opcode != wire.OpSkillCastResult {
				t.Fatalf("cast refused: %+v", start)
			}
			if after, _ := rt.Monsters.Get(testDivision, targets[0].Gid); after.CurrentHP != targets[0].CurrentHP || enterworld.CurrentMP(c) != 150 {
				t.Fatal("the start charged or struck before release")
			}

			result := releasePreparedSkillForTest(t, rt, rt.Now().UnixMilli()+int64(skill.ActionCastingTimeMs)+1)
			p := result.Frames[0].Payload
			const matrix = 11
			if int(p[matrix]) != tc.victims || len(p) != matrix+1+13*tc.victims {
				t.Fatalf("release matrix %x, want %d victims", p, tc.victims)
			}
			var damage []uint32
			for i := 0; i < tc.victims; i++ {
				gid := binary.LittleEndian.Uint32(p[matrix+1+i*13:])
				after, _ := rt.Monsters.Get(testDivision, gid)
				before := uint32(100000)
				if after.CurrentHP >= before {
					t.Fatalf("victim %d undamaged", gid)
				}
				damage = append(damage, before-after.CurrentHP)
			}
			if len(damage) > 1 && damage[1] >= damage[0] {
				t.Fatalf("no falloff: %v", damage)
			}
			if enterworld.CurrentMP(c) != 150-int64(skill.Consumption.MP) {
				t.Fatalf("MP %d, want one charge of %d", enterworld.CurrentMP(c), skill.Consumption.MP)
			}
			if c.MissionInventory[1].StackCount != 1 {
				t.Fatalf("arrows %d, want one spent", c.MissionInventory[1].StackCount)
			}
			if _, ok := findFrame(result.Frames, wire.AvatarInventorySlot7StackCountFrame(1).Opcode); !ok {
				t.Fatal("no slot 7 count for the actor")
			}
		})
	}
}

/*
==================
TestThrownBladesResolveAtReleaseWithoutAmmunition

SKILL_CH_SWORD_SPECIAL_*: a projectile (speed 400) from a sword with no
cnsm. Like the bow's shots it strikes nothing at the start and resolves
its area at release - around the target (shape 6) or the caster (shape 1)
- charging MP once; 587F57 has nothing to debit, so the empty
secondary socket neither refuses the cast nor changes. GEOMGI is the
same thrown blade with a single target (no efr).
==================
*/
func TestThrownBladesResolveAtReleaseWithoutAmmunition(t *testing.T) {
	for _, name := range []string{"SKILL_CH_SWORD_SPECIAL_A_01", "SKILL_CH_SWORD_SPECIAL_B_01", "SKILL_CH_SWORD_GEOMGI_A_01"} {
		t.Run(name, func(t *testing.T) {
			rt, targets := areaFixture(t, 100000)
			c := rt.findCharacter(testDivision, "asd2")
			items := rt.deps.ItemReferences().(staticItemSource)
			sword := *items[c.MissionInventory[0].Codename]
			sword.Codename, sword.RefObjID, sword.TypeIDs[3] = "ITEM_CH_SWORD_01_A", 73, 2
			items[sword.Codename] = &sword
			c.MissionInventory[0].Codename, c.MissionInventory[0].RefObjID, c.MissionInventory[0].TypeFlags = sword.Codename, sword.RefObjID, sword.TypeFlags()
			inventory := len(c.MissionInventory)

			skill := shippedOffense(t, name)
			if !skill.DirectOffensePinned || skill.ProjectileSpeed == 0 || skill.Ammunition.Count != 0 {
				t.Fatalf("not admitted: %+v", skill)
			}
			skill.Consumption.MP = 100 // the shipped cost exceeds the level-1 keeper maximum
			rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
			c.Skills = append(c.Skills, skill.ID)
			c.CurrentMP = testInt64(150)
			mp := enterworld.CurrentMP(c)
			rt.CombatRoll = func() (uint32, error) { return 10, nil }

			start := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: targets[0].Gid}.Encode())
			start = assertAndSeparateActionSession(t, start)
			if len(start.Frames) != 1 || start.Frames[0].Opcode != wire.OpSkillCastResult || start.Frames[0].Payload[0] != 1 {
				t.Fatalf("cast refused: %+v", start)
			}
			if after, _ := rt.Monsters.Get(testDivision, targets[0].Gid); after.CurrentHP != targets[0].CurrentHP || enterworld.CurrentMP(c) != mp {
				t.Fatal("the start charged or struck before release")
			}

			result := releasePreparedSkillForTest(t, rt, rt.Now().UnixMilli()+int64(skill.ActionCastingTimeMs)+1)
			p := result.Frames[0].Payload
			const matrix = 11
			victims := max(1, int(skill.OffensiveArea.MaxTargets))
			if int(p[matrix]) != victims || len(p) != matrix+1+13*victims {
				t.Fatalf("release matrix %x, want %d victims", p, victims)
			}
			var damage []uint32
			for i := 0; i < victims; i++ {
				gid := binary.LittleEndian.Uint32(p[matrix+1+i*13:])
				after, _ := rt.Monsters.Get(testDivision, gid)
				if after.CurrentHP >= 100000 {
					t.Fatalf("victim %d undamaged", gid)
				}
				damage = append(damage, 100000-after.CurrentHP)
			}
			if victims > 1 && damage[1] >= damage[0] {
				t.Fatalf("no falloff: %v", damage)
			}
			if enterworld.CurrentMP(c) != mp-int64(skill.Consumption.MP) {
				t.Fatalf("MP %d, want one charge of %d", enterworld.CurrentMP(c), skill.Consumption.MP)
			}
			if len(c.MissionInventory) != inventory {
				t.Fatal("the inventory changed")
			}
			if _, ok := findFrame(result.Frames, wire.AvatarInventorySlot7StackCountFrame(1).Opcode); ok {
				t.Fatal("a slot 7 count without ammunition")
			}
		})
	}
}
