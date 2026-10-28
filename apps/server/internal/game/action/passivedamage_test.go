package action

import (
	"encoding/binary"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"testing"
)

func TestPassiveDamageChangesCommittedAttackPackets(t *testing.T) {
	var damages [2]uint32
	for index, learned := range []bool{false, true} {
		rt, _, c, target := newCombatTestRuntime(t, 10000)
		passive := shippedOffense(t, "SKILL_EU_WARRIOR_TWOHANDP_ATTACK_A_07")
		base := shippedOffense(t, "SKILL_EU_TSWORD_BASE_01")
		if !passive.PassiveParameters.Pinned || !base.Attack.Parameters.Has(enterworld.ParameterTwoHandPower) {
			t.Fatal("authored program missing")
		}
		skills := rt.deps.SkillData().(staticSkillSource)
		skills[passive.ID] = passive
		// Keep the harness action identity/timing; use the real two-hand
		// attack block and learned passive through the production commit door.
		attack := skills[2]
		attack.Attack = base.Attack
		attack.RequiredWeaponKinds = [2]uint8{8, 255}
		skills[2] = attack
		weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
		weapon.TypeIDs[3] = 8
		c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
		if learned {
			c.Skills = append(c.Skills, passive.ID)
		}
		rt.CombatRoll = func() (uint32, error) { return 100, nil }
		result, decision := rt.acceptSkillCastAt(testDivision, c, rt.characterSnapshot(testDivision, c), wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}, rt.Now().UnixMilli(), rt.handlerDoors())
		if decision != skillCastAccepted || len(result.Frames) == 0 || len(result.Frames[0].Payload) < 34 {
			t.Fatalf("refused %+v", result)
		}
		payload := result.Frames[0].Payload
		for i := 0; i < int(payload[19]); i++ {
			damages[index] += binary.LittleEndian.Uint32(payload[26+i*9:]) >> 8
		}
		after, _ := rt.Monsters.Get(testDivision, target.Gid)
		if target.CurrentHP-after.CurrentHP != damages[index] {
			t.Fatal("serialized damage differs from committed HP")
		}
	}
	if damages[1] <= damages[0] {
		t.Fatalf("passive omitted: %v", damages)
	}
}

func TestPassiveDamageCannotBypassWeaponAvailability(t *testing.T) {
	for _, condition := range []string{"unequipped", "broken", "wrong-kind", "repaired"} {
		t.Run(condition, func(t *testing.T) {
			rt, _, c, target := newCombatTestRuntime(t, 10000)
			passive := shippedOffense(t, "SKILL_EU_WARRIOR_TWOHANDP_ATTACK_A_07")
			skills := rt.deps.SkillData().(staticSkillSource)
			skills[passive.ID] = passive
			c.Skills = append(c.Skills, passive.ID)
			attack := skills[2]
			attack.Attack.Parameters = 1 << enterworld.ParameterTwoHandPower
			attack.RequiredWeaponKinds = [2]uint8{8, 255}
			skills[2] = attack
			weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
			weapon.TypeIDs[3], weapon.MaxDurability = 8, 100
			c.MissionInventory[0].Durability = 0
			if condition != "broken" {
				c.MissionInventory[0].Durability = 100
			}
			if condition == "unequipped" {
				c.MissionInventory[0].Slot = 13
			}
			if condition == "wrong-kind" {
				weapon.TypeIDs[3] = 6
			}
			c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
			rt.CombatRoll = func() (uint32, error) { return 100, nil }
			_, decision := rt.acceptSkillCastAt(testDivision, c, rt.characterSnapshot(testDivision, c), wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}, rt.Now().UnixMilli(), rt.handlerDoors())
			if (decision == skillCastAccepted) != (condition == "repaired") {
				t.Fatalf("%s decision %v", condition, decision)
			}
			after, _ := rt.Monsters.Get(testDivision, target.Gid)
			if condition != "repaired" && after.CurrentHP != target.CurrentHP {
				t.Fatal("refused attack changed HP")
			}
		})
	}
}
