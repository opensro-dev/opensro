/*
===========================================================================

skillshieldtradeoff_test.go - Flying Heaven Art through the real cast owner

Preserve original costs, preparation, cooldown, shield checks and expiry.
Every rank must install and retire both sides of the tradeoff atomically.

===========================================================================
*/
package action

import (
	"fmt"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"testing"
	"time"
)

/*
================
shieldTradeoffFixture
================
*/
func shieldTradeoffFixture(t *testing.T, code string) (*Runtime, *fakeClock, *enterworld.Character, enterworld.SkillRow) {
	t.Helper()
	rt, clock, c, shield, _ := shieldFixture(t)
	row := shippedOffense(t, code)
	if !row.TimedEffect.Pinned || !row.TimedEffect.ShieldTradeoff.Present {
		t.Fatal("unadmitted", code)
	}
	rt.deps.SkillData().(staticSkillSource)[row.ID] = row
	c.Skills = append(c.Skills, row.ID)
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(10000)
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 7, RefObjID: shield.RefObjID, Codename: shield.Codename,
		TypeFlags: shield.TypeFlags(), VarianceBits: "0", Durability: 1, StackCount: 1})
	return rt, clock, c, row
}

/*
================
releaseShieldTradeoff
================
*/
func releaseShieldTradeoff(t *testing.T, rt *Runtime, clock *fakeClock, c *enterworld.Character, row enterworld.SkillRow) {
	t.Helper()
	start := castSelf(rt, c, row.ID)
	if frame, ok := findFrame(start.Frames, wire.OpSkillCastResult); !ok || frame.Payload[0] != 1 || hasSkillEffect(rt, c.Name, row.ID) {
		t.Fatalf("preparation %+v", start)
	}
	clock.Advance(time.Duration(row.ActionCastingTimeMs+1) * time.Millisecond)
	released := releasePreparedSkillForTest(t, rt, clock.NowMs())
	if !hasSkillEffect(rt, c.Name, row.ID) {
		t.Fatalf("release %+v", released)
	}
}

/*
================
TestShieldTradeoffEveryRankUsesNativeCostAndRetires
================
*/
func TestShieldTradeoffEveryRankUsesNativeCostAndRetires(t *testing.T) {
	for _, book := range []struct {
		name    string
		attacks []uint32
	}{
		{"A", []uint32{27, 31, 35, 39, 43, 47}}, {"B", []uint32{53, 57, 61, 65, 69, 73}},
		{"C", []uint32{80, 84, 88, 92, 96, 100}}, {"D", []uint32{107, 111, 115, 119}},
	} {
		for rank, attack := range book.attacks {
			for _, retire := range []string{"expiry", "cancel"} {
				code := fmt.Sprintf("SKILL_CH_SWORD_SHIELDPD_%s_%02d", book.name, rank+1)
				t.Run(code+"/"+retire, func(t *testing.T) {
					rt, clock, c, row := shieldTradeoffFixture(t, code)
					before, _, err := rt.playerCombatStats(testDivision, c)
					if err != nil {
						t.Fatal(err)
					}
					mp := enterworld.CurrentMP(c)
					releaseShieldTradeoff(t, rt, clock, c, row)
					buffed, _, err := rt.playerCombatStats(testDivision, c)
					if err != nil || buffed.PhysicalDefense >= before.PhysicalDefense ||
						float32(buffed.PhysicalAttackMin) != float32(before.PhysicalAttackMin+float64(attack)) ||
						float32(buffed.PhysicalAttackMax) != float32(before.PhysicalAttackMax+float64(attack)) ||
						buffed.MagicalDefense != before.MagicalDefense || buffed.MagicalAttackMin != before.MagicalAttackMin {
						t.Fatalf("tradeoff defense %g -> %g, attack %g -> %g: %v", before.PhysicalDefense, buffed.PhysicalDefense, before.PhysicalAttackMin, buffed.PhysicalAttackMin, err)
					}
					if mp-enterworld.CurrentMP(c) != int64(row.Consumption.MP) {
						t.Fatal("wrong native MP cost")
					}
					for _, effect := range rt.effects.Snapshot(testDivision, c.Name) {
						if effect.SkillID == row.ID && effect.ExpiresAtMs != clock.NowMs()+120000 {
							t.Fatal("wrong deadline", effect)
						}
					}
					clock.Advance(3 * time.Second)
					rt.drainSkillFinalizes(clock.NowMs())
					if retire == "expiry" {
						clock.Advance(120 * time.Second)
					} else {
						rt.HandleTargetInteract(testDivision, c, (wire.CancelActiveEffectRequest{EffectID: row.ID}).Encode())
					}
					rt.TickHook()(clock.NowMs())
					after, _, err := rt.playerCombatStats(testDivision, c)
					if err != nil || hasSkillEffect(rt, c.Name, row.ID) || after.PhysicalDefense != before.PhysicalDefense ||
						after.PhysicalAttackMin != before.PhysicalAttackMin || after.PhysicalAttackMax != before.PhysicalAttackMax {
						t.Fatal("retirement left contributions", err)
					}
				})
			}
		}
	}
}

/*
================
TestShieldTradeoffRequiresItsShieldAtPressAndRelease
================
*/
func TestShieldTradeoffRequiresItsShieldAtPressAndRelease(t *testing.T) {
	for _, phase := range []string{"press", "release", "active unequip", "active break"} {
		t.Run(phase, func(t *testing.T) {
			rt, clock, c, row := shieldTradeoffFixture(t, "SKILL_CH_SWORD_SHIELDPD_A_01")
			mp := enterworld.CurrentMP(c)
			if phase == "press" {
				c.MissionInventory[len(c.MissionInventory)-1].Slot = 20
				out := castSelf(rt, c, row.ID)
				if frame, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok || frame.Payload[0] != 2 || frame.Payload[1] != 13 {
					t.Fatalf("missing shield admitted %+v", out)
				}
			} else if phase == "release" {
				castSelf(rt, c, row.ID)
				c.MissionInventory[len(c.MissionInventory)-1].Slot = 20
				clock.Advance(2 * time.Second)
				releasePreparedSkillForTest(t, rt, clock.NowMs())
			} else {
				releaseShieldTradeoff(t, rt, clock, c, row)
				clock.Advance(3 * time.Second)
				rt.drainSkillFinalizes(clock.NowMs())
				if phase == "active unequip" {
					out := rt.HandleItemMove(testDivision, c, encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: 7, DestSlot: 20, Quantity: 1}))
					if len(out.Frames) == 0 || out.Frames[0].Payload[0] != 1 {
						t.Fatalf("unequip %+v", out)
					}
				} else {
					rt.WearRoll = func() (uint32, error) { return 0, nil }
					rt.applyEquipmentWear(testDivision, c, wearTally{shield: 1})
					if durabilityAt(c, 7) != 0 {
						t.Fatal("shield did not break")
					}
				}
			}
			if hasSkillEffect(rt, c.Name, row.ID) {
				t.Fatal("effect outlived its shield")
			}
			if (phase == "press" || phase == "release") && enterworld.CurrentMP(c) != mp {
				t.Fatal("refused skill charged MP")
			}
		})
	}
}

/*
================
TestShieldTradeoffCooldownSurvivesCancellation
================
*/
func TestShieldTradeoffCooldownSurvivesCancellation(t *testing.T) {
	rt, clock, c, row := shieldTradeoffFixture(t, "SKILL_CH_SWORD_SHIELDPD_A_01")
	started := clock.NowMs()
	releaseShieldTradeoff(t, rt, clock, c, row)
	clock.Advance(3 * time.Second)
	rt.drainSkillFinalizes(clock.NowMs())
	rt.HandleTargetInteract(testDivision, c, (wire.CancelActiveEffectRequest{EffectID: row.ID}).Encode())
	rt.TickHook()(clock.NowMs())
	mp := enterworld.CurrentMP(c)
	out := castSelf(rt, c, row.ID)
	frame, ok := findFrame(out.Frames, wire.OpSkillCastResult)
	if !ok || len(frame.Payload) != 2 || frame.Payload[0] != 2 || frame.Payload[1] != 5 || hasSkillEffect(rt, c.Name, row.ID) || enterworld.CurrentMP(c) != mp {
		t.Fatalf("cooldown bypass %+v", out)
	}
	if c.OffensiveSkillCooldowns[row.Group] != started+180000 {
		t.Fatal("native cooldown changed", c.OffensiveSkillCooldowns)
	}
}
