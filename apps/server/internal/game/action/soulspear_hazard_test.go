/*
===========================================================================

soulspear_hazard_test.go - Soul Spear Emperor's second stage under fire

#bug-reports: the reporter's D2 was lost "about every 10 uses" while a
Shauer (MOB_RM_GOAT_CLON) was hitting the caster and auto-potions were
firing. Natively a hit is action state 0xB, which clears no cast state, and
a potion is an item use that never touches the attack command. Neither a
Shauer attack landing between the stages, nor an HP potion used on each
tick between them, nor both together with an active imbue, may cost the
server-owned D2 stage.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// soulSpearPotionSlot is a free bag slot for the fixture's HP potions.
const soulSpearPotionSlot = 30

/*
================
TestSoulSpearSecondStageUnderHitsAndPotions
================
*/
func TestSoulSpearSecondStageUnderHitsAndPotions(t *testing.T) {
	for _, attack := range []string{"MSKILL_RM_GOAT_CLON_ATTACK01", "MSKILL_RM_GOAT_CLON_ATTACK02"} {
		for _, hazard := range []struct {
			name        string
			hit, potion bool
			imbue       bool
		}{
			{"hit", true, false, false},
			{"potion", false, true, false},
			{"hit+potion", true, true, false},
			{"hit+potion+imbue", true, true, true},
		} {
			t.Run(fmt.Sprintf("%s/%s", attack, hazard.name), func(t *testing.T) {
				rt, targets := areaFixture(t, 1000000)
				ch := rt.findCharacter(testDivision, "asd2")
				root := shippedOffense(t, "SKILL_CH_SPEAR_STUN_D_06")
				second := shippedOffense(t, "SKILL_CH_SPEAR_STUN_D2_06")
				imbue := shippedOffense(t, "SKILL_CH_LIGHTNING_GIGONGTA_A_01")
				shauer := shippedOffense(t, attack)
				skills := rt.deps.SkillData().(staticSkillSource)
				skills[root.ID], skills[second.ID], skills[imbue.ID], skills[shauer.ID] = root, second, imbue, shauer
				ch.Skills = append(ch.Skills, root.ID, imbue.ID)
				ch.Masteries = append(ch.Masteries, enterworld.CharacterMastery{ID: 258, Level: 90},
					enterworld.CharacterMastery{ID: imbue.Masteries[0].ID, Level: 90})
				ch.Level, ch.MaxLevel = testInt64(90), testInt64(90)
				ch.Strength, ch.Intellect = testInt64(2000), testInt64(2000)
				ch.CurrentMP = testInt64(100000)
				ch.CurrentHP = testInt64(30000)
				items := rt.deps.ItemReferences().(staticItemSource)
				spear := *items[ch.MissionInventory[0].Codename]
				spear.Codename, spear.RefObjID, spear.TypeIDs[3] = "ITEM_CH_SPEAR_01_A", 73, 4
				items[spear.Codename] = &spear
				ch.MissionInventory[0].Codename, ch.MissionInventory[0].RefObjID, ch.MissionInventory[0].TypeFlags = spear.Codename, spear.RefObjID, spear.TypeFlags()
				potion := *testItems()["ITEM_ETC_HP_POTION_01"]
				items[potion.Codename] = &potion
				ch.MissionInventory = append(ch.MissionInventory, enterworld.InventoryRow{
					Slot: soulSpearPotionSlot, RefObjID: potion.RefObjID, Codename: potion.Codename,
					TypeFlags: potion.TypeFlags(), StackCount: 50,
				})
				rt.CombatRoll = func() (uint32, error) { return 16000, nil }
				// The Shauer stands beside the struck target and attacks the caster.
				attacker := targets[1]
				attacker.Ref.DefaultSkillIDs[0] = shauer.ID
				casterGid := enterworld.ObjectIDForCharacter(ch)
				if hazard.imbue {
					r := rt.HandleTargetInteract(testDivision, ch, wire.SkillAction{ActionId: imbue.ID}.Encode())
					if len(r.Frames) == 0 || r.Frames[0].Payload[0] != 1 {
						t.Fatalf("imbue refused: %s %+v", r.DiagnosticRefusal, r.Frames)
					}
				}
				start := rt.HandleTargetInteract(testDivision, ch, wire.SkillAction{ActionId: root.ID, HasTarget: true, TargetGid: targets[0].Gid}.Encode())
				if len(start.Frames) == 0 || start.Frames[0].Opcode != wire.OpSkillCastResult || start.Frames[0].Payload[0] != 1 {
					t.Fatalf("root refused %s %+v", start.DiagnosticRefusal, start.Frames)
				}
				base := rt.Now()
				staged, hitAccepted, potions := false, false, 0
				hpBefore := enterworld.CurrentHP(ch)
				for tick := 1; tick <= 60 && !staged; tick++ {
					now := base.Add(time.Duration(tick) * 100 * time.Millisecond).UnixMilli()
					if hazard.hit && tick == 1 {
						hitAccepted = rt.MonsterBasicAttack(testDivision, attacker, casterGid, shauer.ID, now).Accepted
					}
					if hazard.potion {
						request := wire.NewWriter(3).U8(soulSpearPotionSlot).U16(potion.TypeFlags()).Payload()
						if r := rt.HandleItemUse(testDivision, ch, request); len(r.Frames) > 0 && r.Frames[0].Payload[0] == 1 {
							potions++
						}
					}
					for _, burst := range append(rt.MonsterActionTickHook()(now), rt.TickHook()(now)...) {
						for _, frame := range burst.Frames {
							if frame.Opcode == wire.OpSkillCastResult && len(frame.Payload) >= 6 && frame.Payload[0] == 1 &&
								binary.LittleEndian.Uint32(frame.Payload[2:6]) == second.ID {
								staged = true
							}
						}
					}
				}
				if hazard.hit && !hitAccepted {
					t.Fatal("the Shauer's attack was not admitted: the hazard never ran")
				}
				if hazard.hit && !hazard.potion && enterworld.CurrentHP(ch) >= hpBefore {
					t.Fatalf("the Shauer's attack never landed (hp %d -> %d)", hpBefore, enterworld.CurrentHP(ch))
				}
				if hazard.potion && potions == 0 {
					t.Fatal("no potion was used: the hazard never ran")
				}
				if !staged {
					in, has := rt.combatIntentFor(testDivision, ch.Name)
					t.Fatalf("D2 never ran (intent=%v skill=%d root=%d, hp %d -> %d, potions %d)",
						has, in.SkillID, in.ComboRootID, hpBefore, enterworld.CurrentHP(ch), potions)
				}
			})
		}
	}
}
