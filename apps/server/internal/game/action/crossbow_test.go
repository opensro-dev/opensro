/*
===========================================================================

crossbow_test.go - authored Rogue shots through the production action owner

These cases exercise complete linked casts, bolt debits, range, cancellation,
and projectile retention. They use the same catalog as the running server.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	crossbowTestBolts = 20
	crossbowTestMP    = 10000
	crossbowTestHP    = 1000000
)

/*
================
crossbowFixture

Keep the fixture's level-data contract while supplying a European weapon,
the authored complete skill graph, and enough resources for high-level rows.
================
*/
func crossbowFixture(t *testing.T, code string) (*Runtime, *fakeClock, *enterworld.Character, uint32, []enterworld.SkillRow) {
	t.Helper()
	source := shippedSkillSource(t)
	root, ok := source.SkillByCodename(code)
	if !ok {
		t.Fatalf("missing skill %s", code)
	}
	stages, ok := enterworld.OffensiveSequence(source, root.ID)
	if !ok {
		t.Fatalf("incomplete skill %s: %s", code, root.OffenseRefusal)
	}
	rt, clock, c, target := newCombatTestRuntime(t, crossbowTestHP)
	c.RaceIndex = testInt64(enterworld.RaceEurope)
	c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	c.Skills = []uint32{root.ID}
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(crossbowTestMP)
	items := rt.deps.ItemReferences().(staticItemSource)
	weapon := items[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 12
	weapon.Combat.ActionRange = 180
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	bolt := &enterworld.ItemRef{RefObjID: 62002, Codename: "ITEM_ETC_AMMO_BOLT_01", TypeIDs: [4]int64{3, 3, 4, 2}}
	items[bolt.Codename] = bolt
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
		Slot: 7, RefObjID: bolt.RefObjID, Codename: bolt.Codename, TypeFlags: bolt.TypeFlags(), StackCount: crossbowTestBolts,
	})
	for _, stage := range stages {
		rt.deps.SkillData().(staticSkillSource)[stage.ID] = stage
	}
	rt.CombatRoll = func() (uint32, error) { return 10, nil }
	return rt, clock, c, target.Gid, stages
}

/*
================
TestCrossbowAuthoredFamily

Every rank must compile as a complete graph. Running every variant through
the command/tick owner proves that linked stages are executed, not just parsed.
================
*/
func TestCrossbowAuthoredFamily(t *testing.T) {
	source := shippedSkillSource(t)
	roots := 0
	for id := uint32(1); id < 65536; id++ {
		row, ok := source.SkillByID(id)
		if !ok || row.ChainSub || !strings.HasPrefix(row.Codename, "SKILL_EU_ROG_BOWA_") {
			continue
		}
		roots++
		if _, ok := enterworld.OffensiveSequence(source, id); !ok {
			t.Errorf("%s: %s", row.Codename, row.OffenseRefusal)
		}
	}
	if roots != 72 {
		t.Fatalf("catalog has %d roots, want 72", roots)
	}
	for _, variant := range []string{"POWER_A", "POWER_B", "FAST_A", "FAST_B", "RANGE_A", "RANGE_B", "KNOCK_A", "KNOCK_B"} {
		t.Run(variant, func(t *testing.T) {
			rt, clock, c, target, stages := crossbowFixture(t, "SKILL_EU_ROG_BOWA_"+variant+"_01")
			root := stages[0]
			result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: root.ID, HasTarget: true, TargetGid: target}.Encode())
			start, ok := findFrame(result.Frames, wire.OpSkillCastResult)
			if !ok || len(start.Payload) < 19 || start.Payload[0] != 1 {
				t.Fatalf("cast refused: %+v", result)
			}
			if root.ActionCastingTimeMs != 0 && (enterworld.CurrentMP(c) != crossbowTestMP || c.MissionInventory[1].StackCount != crossbowTestBolts) {
				t.Fatal("preparation charged resources")
			}
			token := binary.LittleEndian.Uint32(start.Payload[10:])
			closes := 0
			for tick := 1; tick <= 800; tick++ {
				for _, batch := range rt.TickHook()(clock.At(time.Duration(tick) * 10 * time.Millisecond).UnixMilli()) {
					for _, f := range batch.Frames {
						if f.Opcode == wire.OpSkillEffectControl && len(f.Payload) == 6 && f.Payload[0] == 2 {
							if binary.LittleEndian.Uint32(f.Payload[2:]) != token {
								t.Fatal("linked stage closed a token the client does not own")
							}
							closes++
						}
						if f.Opcode == wire.OpAvatarInventorySlot7StackCount && (batch.OnlyCharacterID != c.ID || batch.SourceGID != 0) {
							t.Fatal("bolt count leaked into observer routing")
						}
					}
				}
			}
			spent := int64(0)
			for _, stage := range stages {
				spent += int64(stage.Ammunition.Count) * int64(stage.Attack.ImpactCount)
			}
			if got := c.MissionInventory[1].StackCount; got != crossbowTestBolts-spent {
				t.Fatalf("bolts=%d, want %d across %d stages", got, crossbowTestBolts-spent, len(stages))
			}
			if enterworld.CurrentMP(c) != crossbowTestMP-int64(root.Consumption.MP) {
				t.Fatalf("MP=%d; root cost=%d", enterworld.CurrentMP(c), root.Consumption.MP)
			}
			after, _ := rt.Monsters.Get(testDivision, target)
			if after.CurrentHP >= crossbowTestHP || closes != 1 || rt.hasOpenSkillCast(testDivision, c.Name) {
				t.Fatalf("HP=%d closes=%d; cast did not complete", after.CurrentHP, closes)
			}
		})
	}
}

/*
================
TestCrossbowRangeAddend

ru is added only to equipment-derived range, before CBRA. An explicit skill
range bypasses ru (4AE838); it still receives the requested CBRA modifier.
================
*/
func TestCrossbowRangeAddend(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_ROG_BOWA_FAST_A_01")
	var caster combat.Stats
	caster.SkillParameters[enterworld.ParameterCrossbowRange] = 40
	loadout := combat.Loadout{HasWeapon: true, WeaponKind: 12, ActionRange: 180}
	if got := skillActionReach(skill, loadout, caster); got != 370 {
		t.Fatalf("equipment + ru + CBRA = %g, want 370", got)
	}
	skill.ActionRange = 150
	if got := skillActionReach(skill, loadout, caster); got != 190 {
		t.Fatalf("explicit range + CBRA = %g, want 190", got)
	}
}

/*
================
TestCrossbowPreparationCancellation

Wrong ammunition refuses before preparation. Removing bolts or cancelling
after acceptance must retire the cast without spending MP or damaging a target.
================
*/
func TestCrossbowPreparationCancellation(t *testing.T) {
	for _, mode := range []string{"arrows", "removed", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			rt, clock, c, target, stages := crossbowFixture(t, "SKILL_EU_ROG_BOWA_RANGE_A_01")
			root := stages[0]
			if mode == "arrows" {
				rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[1].Codename].TypeIDs[3] = 1
			}
			_, decision := rt.acceptSkillCastAt(testDivision, c, rt.characterSnapshot(testDivision, c), wire.SkillAction{ActionId: root.ID, HasTarget: true, TargetGid: target}, clock.NowMs())
			if mode == "arrows" {
				if decision != skillCastRefused {
					t.Fatal("crossbow accepted arrows")
				}
			} else if decision != skillCastAccepted {
				t.Fatal("valid preparation refused")
			}
			if mode == "removed" {
				c.MissionInventory = c.MissionInventory[:1]
			}
			if mode == "cancel" {
				rt.cancelPreparingProjectile(testDivision, c.Name)
			}
			rt.advanceProjectileCasts(clock.NowMs() + int64(root.ActionCastingTimeMs) + 1)
			after, _ := rt.Monsters.Get(testDivision, target)
			if after.CurrentHP != crossbowTestHP || enterworld.CurrentMP(c) != crossbowTestMP || len(rt.pendingProjectileCasts) != 0 {
				t.Fatal("refused or cancelled preparation committed gameplay")
			}
		})
	}
}

/*
================
TestCrossbowInstantFlightRetention

A shot can outlive its action recovery. Extend the actor token through flight
for both single-target Fast Shot and its area upgrade, even with no preparation.
================
*/
func TestCrossbowInstantFlightRetention(t *testing.T) {
	for _, code := range []string{"SKILL_EU_ROG_BOWA_FAST_A_01", "SKILL_EU_ROG_BOWA_FAST_B_01"} {
		t.Run(code, func(t *testing.T) {
			rt, clock, c, target, stages := crossbowFixture(t, code)
			skill := stages[0]
			skill.ActionDurationMs = 1
			rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
			*c.World.Spawn.X -= 100
			before, _ := rt.Monsters.Get(testDivision, target)
			flight := projectileFlightMs(simulation.SeedWorldState(c).Spawn, simulation.Spawn{RegionID: before.Spawn.RegionID, X: before.Spawn.X, Y: before.Spawn.Y, Z: before.Spawn.Z}, skill.ProjectileSpeed)
			result, decision := rt.acceptSkillCastAt(testDivision, c, rt.characterSnapshot(testDivision, c), wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target}, clock.NowMs())
			if decision != skillCastAccepted {
				t.Fatalf("shot refused: %+v", result)
			}
			rt.drainSkillFinalizes(clock.NowMs() + flight)
			if !rt.hasOpenSkillCast(testDivision, c.Name) {
				t.Fatal("token closed before strict flight boundary")
			}
			rt.drainSkillFinalizes(clock.NowMs() + flight + 1)
			if rt.hasOpenSkillCast(testDivision, c.Name) {
				t.Fatal("token retained after flight")
			}
		})
	}
}

/*
================
TestCrossbowLastBoltAndInterruptedChain

Rapid Shot's three-bolt debit floors at zero. A chain that loses ammunition
after its root may not invent a free follow-up or debit the root MP again.
================
*/
func TestCrossbowLastBoltAndInterruptedChain(t *testing.T) {
	for _, code := range []string{"SKILL_EU_ROG_BOWA_FAST_B_01", "SKILL_EU_ROG_BOWA_POWER_B_01"} {
		t.Run(code, func(t *testing.T) {
			rt, clock, c, target, stages := crossbowFixture(t, code)
			c.MissionInventory[1].StackCount = 1
			root := stages[0]
			rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: root.ID, HasTarget: true, TargetGid: target}.Encode())
			for tick := 1; tick <= 400; tick++ {
				rt.TickHook()(clock.At(time.Duration(tick) * 10 * time.Millisecond).UnixMilli())
			}
			if len(c.MissionInventory) != 1 || enterworld.CurrentMP(c) != crossbowTestMP-int64(root.Consumption.MP) {
				t.Fatalf("last-bolt resources: %+v MP=%d", c.MissionInventory, enterworld.CurrentMP(c))
			}
			if rt.hasOpenSkillCast(testDivision, c.Name) || len(rt.combatIntentSnapshot()) != 0 {
				t.Fatal("exhausted ammunition left a live cast or continuation")
			}
		})
	}
}
