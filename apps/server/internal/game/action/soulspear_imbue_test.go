/*
===========================================================================

soulspear_imbue_test.go - Soul Spear Emperor's second stage with an imbue

#bug-reports: Soul Spear Emperor (SKILL_CH_SPEAR_STUN_D) sometimes loses
its D2 stage, and players linked it to an active imbue. An imbue is an
instant self effect that keeps the current attack (the reference server dispatches it
before the command queue), so neither an active imbue nor one pressed
between the stages may cancel the server-owned D2 stage.

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

/*
================
TestSoulSpearSecondStageWithAnImbue

Each Chinese force imbue, active before the root (tick -1) or pressed on
one of the ticks between the stages: D2 is still admitted.
================
*/
func TestSoulSpearSecondStageWithAnImbue(t *testing.T) {
	for _, imbueAt := range []int{-1, 1, 2, 3, 5} {
		for _, codename := range []string{"SKILL_CH_FIRE_GIGONGTA_A_01", "SKILL_CH_LIGHTNING_GIGONGTA_A_01", "SKILL_CH_COLD_GIGONGTA_A_01"} {
			t.Run(fmt.Sprintf("%s/tick%d", codename, imbueAt), func(t *testing.T) {
				rt, targets := areaFixture(t, 1000000)
				ch := rt.findCharacter(testDivision, "asd2")
				root := shippedOffense(t, "SKILL_CH_SPEAR_STUN_D_06")
				second := shippedOffense(t, "SKILL_CH_SPEAR_STUN_D2_06")
				imbue := shippedOffense(t, codename)
				skills := rt.deps.SkillData().(staticSkillSource)
				skills[root.ID], skills[second.ID], skills[imbue.ID] = root, second, imbue
				ch.Skills = append(ch.Skills, root.ID, imbue.ID)
				ch.Masteries = append(ch.Masteries, enterworld.CharacterMastery{ID: 258, Level: 90},
					enterworld.CharacterMastery{ID: imbue.Masteries[0].ID, Level: 90})
				ch.Level, ch.MaxLevel = testInt64(90), testInt64(90)
				ch.Strength, ch.Intellect = testInt64(2000), testInt64(2000)
				ch.CurrentMP = testInt64(100000)
				items := rt.deps.ItemReferences().(staticItemSource)
				spear := *items[ch.MissionInventory[0].Codename]
				spear.Codename, spear.RefObjID, spear.TypeIDs[3] = "ITEM_CH_SPEAR_01_A", 73, 4
				items[spear.Codename] = &spear
				ch.MissionInventory[0].Codename, ch.MissionInventory[0].RefObjID, ch.MissionInventory[0].TypeFlags = spear.Codename, spear.RefObjID, spear.TypeFlags()
				rt.CombatRoll = func() (uint32, error) { return 16000, nil }
				press := func() OpResult {
					return rt.HandleTargetInteract(testDivision, ch, wire.SkillAction{ActionId: imbue.ID}.Encode())
				}
				if imbueAt < 0 {
					if r := press(); len(r.Frames) == 0 || r.Frames[0].Payload[0] != 1 {
						t.Fatalf("imbue refused: %s %+v", r.DiagnosticRefusal, r.Frames)
					}
				}
				start := rt.HandleTargetInteract(testDivision, ch, wire.SkillAction{ActionId: root.ID, HasTarget: true, TargetGid: targets[0].Gid}.Encode())
				if len(start.Frames) == 0 || start.Frames[0].Opcode != wire.OpSkillCastResult || start.Frames[0].Payload[0] != 1 {
					t.Fatalf("root refused %s %+v", start.DiagnosticRefusal, start.Frames)
				}
				base := rt.Now()
				staged := false
				for tick := 1; tick <= 60 && !staged; tick++ {
					if tick == imbueAt {
						if r := press(); len(r.Frames) == 0 || r.Frames[0].Payload[0] != 1 {
							t.Fatalf("imbue refused mid-chain: %s", r.DiagnosticRefusal)
						}
					}
					for _, burst := range rt.TickHook()(base.Add(time.Duration(tick) * 100 * time.Millisecond).UnixMilli()) {
						for _, frame := range burst.Frames {
							if frame.Opcode == wire.OpSkillCastResult && len(frame.Payload) >= 6 && frame.Payload[0] == 1 &&
								binary.LittleEndian.Uint32(frame.Payload[2:6]) == second.ID {
								staged = true
							}
						}
					}
				}
				if !staged {
					in, has := rt.combatIntentFor(testDivision, ch.Name)
					t.Fatalf("D2 never ran (intent=%v skill=%d root=%d)", has, in.SkillID, in.ComboRootID)
				}
			})
		}
	}
}
