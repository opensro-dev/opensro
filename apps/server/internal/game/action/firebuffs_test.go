/*
===========================================================================

firebuffs_test.go - native Fire buffs through the gameplay action owner

Cover every Fire Protection and Flame Body rank with original costs,
lifetimes and recipient modifiers. The revealing buffs select nearby
non-party characters; the Fire passive adds its own physical damage bonus.
Fire Shield, imbue damage and wall absorption have separate gameplay suites.

===========================================================================
*/
package action

import (
	"fmt"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
fireTimedBuffCodes
================
*/
func fireTimedBuffCodes() []string {
	var codes []string
	for _, family := range []struct {
		name  string
		ranks [4]int
	}{
		{"GANGGI", [4]int{9, 9, 9, 7}},
		{"GONGUP", [4]int{3, 3, 3, 2}},
	} {
		for book, count := range family.ranks {
			for rank := 1; rank <= count; rank++ {
				codes = append(codes, fmt.Sprintf("SKILL_CH_FIRE_%s_%c_%02d", family.name, 'A'+book, rank))
			}
		}
	}
	return codes
}

/*
================
TestFireTimedBuffRanksUseOriginalCostModifiersAndRetirement

59520C installs defp on magical defense only; 595A97 installs dru on
physical damage parameters 80/81. Both contributions belong to the instance.
================
*/
func TestFireTimedBuffRanksUseOriginalCostModifiersAndRetirement(t *testing.T) {
	for _, code := range fireTimedBuffCodes() {
		for _, retirement := range []string{"expiry", "cancel"} {
			t.Run(code+"/"+retirement, func(t *testing.T) {
				rt, clock, c, _ := newCombatTestRuntime(t, 100000)
				row := shippedOffense(t, code)
				if !row.TimedEffect.Pinned || row.SpawnStatus || row.TimedEffect.Targeted {
					t.Fatalf("incomplete self buff %+v", row)
				}
				// Afford all original costs while retaining the fixture's level-1 row.
				c.Intellect = testInt64(2000)
				c.CurrentMP = testInt64(10000)
				c.Skills = append(c.Skills, row.ID)
				rt.deps.SkillData().(staticSkillSource)[row.ID] = row
				before, _, err := rt.playerCombatStats(testDivision, c)
				if err != nil {
					t.Fatal(err)
				}
				mp := enterworld.CurrentMP(c)
				out := castSelf(rt, c, row.ID)
				if frame, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok || frame.Payload[0] != 1 || out.DiagnosticRefusal != "" {
					t.Fatalf("preparation refused: %+v", out)
				}
				if hasSkillEffect(rt, c.Name, row.ID) || enterworld.CurrentMP(c) != mp {
					t.Fatal("preparation installed or charged the buff early")
				}
				clock.Advance(time.Duration(row.ActionCastingTimeMs+1) * time.Millisecond)
				releasePreparedSkillForTest(t, rt, clock.NowMs())
				effects := rt.effects.Snapshot(testDivision, c.Name)
				if len(effects) != 1 || effects[0].SkillID != row.ID || effects[0].ExpiresAtMs != clock.NowMs()+int64(row.EffectDurationMs) {
					t.Fatalf("native lifetime or ownership differs: %+v", effects)
				}
				if cost := mp - enterworld.CurrentMP(c); cost != int64(row.Consumption.MP) {
					t.Fatalf("MP charged %d, want original %d", cost, row.Consumption.MP)
				}
				after, _, err := rt.playerCombatStats(testDivision, c)
				if err != nil {
					t.Fatal(err)
				}
				for _, parameter := range []uint16{5, 6, 0x80, 0x81, 0x82, 0x83} {
					was, _ := before.Param(parameter)
					now, _ := after.Param(parameter)
					var gain float32
					if parameter == 6 {
						gain = float32(row.TimedEffect.Magical)
					}
					if parameter == 0x80 || parameter == 0x81 {
						gain = float32(row.BuffModifiers.DruWords[0])
					}
					if now != was+gain {
						t.Fatalf("parameter %x = %v, want %v + %v", parameter, now, was, gain)
					}
				}
				if retirement == "expiry" {
					clock.Advance(time.Duration(row.EffectDurationMs+1) * time.Millisecond)
					rt.effects.Expire(clock.NowMs())
				} else {
					clock.Advance(time.Duration(row.ActionDurationMs+1) * time.Millisecond)
					rt.drainSkillFinalizes(clock.NowMs())
					rt.HandleTargetInteract(testDivision, c, wire.CancelActiveEffectRequest{
						EffectID: row.ID, InstanceToken: effects[0].InstanceToken,
					}.Encode())
				}
				rt.drainStoppedCharacterEffects()
				if hasSkillEffect(rt, c.Name, row.ID) {
					t.Fatal("retired buff remains attached")
				}
				retired, _, err := rt.playerCombatStats(testDivision, c)
				if err != nil {
					t.Fatal(err)
				}
				for _, parameter := range []uint16{5, 6, 0x80, 0x81, 0x82, 0x83} {
					was, _ := before.Param(parameter)
					now, _ := retired.Param(parameter)
					if now != was {
						t.Fatalf("retired parameter %x = %v, want %v", parameter, now, was)
					}
				}
			})
		}
	}
}

/*
================
TestFireDetectionRanksRespectOriginalRecipientsAndRange

The original efr select 26 reveals non-party characters within its radius;
the source does not receive the recipient's dttp instance.
================
*/
func TestFireDetectionRanksRespectOriginalRecipientsAndRange(t *testing.T) {
	for _, family := range []struct {
		name  string
		book  string
		ranks int
		mask  uint32
	}{
		{"DESCRY", "A", 5, 5}, {"DESCRY", "B", 2, 5}, {"DETECT", "A", 7, 6},
	} {
		for rank := 1; rank <= family.ranks; rank++ {
			code := fmt.Sprintf("SKILL_CH_FIRE_%s_%s_%02d", family.name, family.book, rank)
			t.Run(code, func(t *testing.T) {
				row := shippedOffense(t, code)
				rt, clock, c := concealmentFixture(t, row.ID)
				c.Intellect = testInt64(2000)
				c.CurrentMP = testInt64(10000)
				rt.deps.SkillData().(staticSkillSource)[row.ID] = row
				if !row.Concealment.Pinned || row.Concealment.Reveal.Mask != family.mask || row.Concealment.Area.Select != 26 {
					t.Fatalf("native detection admission %+v", row.Concealment)
				}
				stranger := nearbyCharacter(rt, c, 11, "stranger", 30)
				mate := nearbyCharacter(rt, c, 12, "mate", 30)
				radius, ok := rt.deps.CharacterBodyRadius(stranger)
				if !ok {
					t.Fatal("missing original character body radius")
				}
				edge := nearbyCharacter(rt, c, 13, "edge", float64(row.Concealment.Area.Radius)+radius)
				far := nearbyCharacter(rt, c, 14, "far", float64(row.Concealment.Area.Radius)+radius+1)
				rt.RewardParties = func(string) []RewardParty {
					return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(mate)}}}
				}
				out := castSelf(rt, c, row.ID)
				if frame, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok || frame.Payload[0] != 1 || out.DiagnosticRefusal != "" {
					t.Fatalf("detection cast refused %+v", out)
				}
				clock.Advance(time.Duration(row.ActionCastingTimeMs+1) * time.Millisecond)
				releasePreparedSkillForTest(t, rt, clock.NowMs())
				for name, want := range map[string]bool{c.Name: false, stranger.Name: true, mate.Name: false, edge.Name: true, far.Name: false} {
					if got := hasSkillEffect(rt, name, row.ID); got != want {
						t.Fatalf("%s receives reveal %v, want %v", name, got, want)
					}
				}
				clock.Advance(time.Duration(row.EffectDurationMs+1) * time.Millisecond)
				rt.effects.Expire(clock.NowMs())
				rt.drainStoppedCharacterEffects()
				if hasSkillEffect(rt, stranger.Name, row.ID) {
					t.Fatal("expired reveal remains installed")
				}
			})
		}
	}
}

/*
================
TestFirePassiveRanksAddOnlyTheirPhysicalDamageBonus
================
*/
func TestFirePassiveRanksAddOnlyTheirPhysicalDamageBonus(t *testing.T) {
	for rank := 1; rank <= 9; rank++ {
		code := fmt.Sprintf("SKILL_CH_FIRE_PASSIVE_A_%02d", rank)
		t.Run(code, func(t *testing.T) {
			rt, _, c, _ := newCombatTestRuntime(t, 100000)
			before, _, err := rt.playerCombatStats(testDivision, c)
			if err != nil {
				t.Fatal(err)
			}
			row := shippedOffense(t, code)
			if !row.PassiveParameters.Pinned || row.PassiveParameters.Dru != [2]uint32{uint32(rank), 0} {
				t.Fatalf("native passive program %+v", row.PassiveParameters)
			}
			rt.deps.SkillData().(staticSkillSource)[row.ID] = row
			c.Skills = append(c.Skills, row.ID)
			after, _, err := rt.playerCombatStats(testDivision, c)
			if err != nil {
				t.Fatal(err)
			}
			for _, parameter := range []uint16{0x80, 0x81, 0x82, 0x83} {
				was, _ := before.Param(parameter)
				now, _ := after.Param(parameter)
				var gain float32
				if parameter == 0x80 || parameter == 0x81 {
					gain = float32(rank)
				}
				if now != was+gain {
					t.Fatalf("parameter %x = %v, want %v + %v", parameter, now, was, gain)
				}
			}
		})
	}
}
