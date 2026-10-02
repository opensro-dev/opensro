/*
===========================================================================

scorn_test.go - Scorn and Gross Scorn through the live action owner

The effect constrains commands without reducing HP. Its instance owns expiry,
replacement and source-presence retirement.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
scornOpponentPair

Level-one stat fixtures can fight under opposing capes. Place them in an
authored battlefield; the generic support fixture starts inside Jangan.
================
*/
func scornOpponentPair(t *testing.T, skill enterworld.SkillRow) supportPair {
	t.Helper()
	p := newSupportPair(t, skill)
	p.rt.RewardParties = nil
	items := p.rt.deps.ItemReferences().(staticItemSource)
	weapon := items[p.c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 13
	p.c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	for i, actor := range []*enterworld.Character{p.c, p.m} {
		cape := &enterworld.ItemRef{
			RefObjID: uint32(63000 + i), Codename: fmt.Sprintf("TEST_PVP_CAPE_%d", i),
			TypeIDs:      [4]int64{3, 1, 7, 5},
			NativeFields: enterworld.NewNativeFields(map[string]float64{freeBattleGroupField: float64(i + 1)}),
		}
		items[cape.Codename] = cape
		actor.MissionInventory = append(append([]enterworld.InventoryRow(nil), actor.MissionInventory...), enterworld.InventoryRow{
			Slot: 8, RefObjID: cape.RefObjID, Codename: cape.Codename, TypeFlags: cape.TypeFlags(), StackCount: 1,
		})
		actor.World.Spawn.RegionID = testInt64(0x62a6)
		p.rt.Worlds.Update(simulation.WorldKey(testDivision, actor.Name), func() simulation.WorldState {
			return simulation.SeedWorldState(actor)
		}, func(w *simulation.WorldState) { w.Spawn.RegionID = 0x62a6 })
	}
	return p
}

/*
================
TestScornCatalog
================
*/
func TestScornCatalog(t *testing.T) {
	source := shippedSkillSource(t)
	count := 0
	for id := uint32(1); id < 65536; id++ {
		row, ok := source.SkillByID(id)
		if !ok || !strings.HasPrefix(row.Codename, "SKILL_EU_ROG_STEALTHA_CHANGE_") {
			continue
		}
		count++
		if !row.TimedEffect.Pinned || !row.TimedEffect.ForcedTarget || !row.TimedEffect.Targeted || !row.VoluntaryCancelBlocked {
			t.Fatalf("Scorn rank %s: %+v", row.Codename, row.TimedEffect)
		}
	}
	if count != 6 {
		t.Fatalf("Scorn ranks=%d", count)
	}
}

/*
================
TestScornConstrainsSharedAdmissionAndExpires
================
*/
func TestScornConstrainsSharedAdmissionAndExpires(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_ROG_STEALTHA_CHANGE_A_01")
	p := scornOpponentPair(t, skill)
	hp := enterworld.CurrentHP(p.m)
	p.rt.setCombatIntent(basicAttackIntent{DivisionID: testDivision, CharacterName: p.m.Name, TargetGid: 12345})
	out := p.cast(skill.ID)
	if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok {
		t.Fatalf("Scorn refused: %+v", out)
	}
	gid := enterworld.ObjectIDForCharacter(p.c)
	if got := p.rt.effects.ForcedTarget(testDivision, p.m.Name, p.clock.NowMs()); got != gid {
		t.Fatalf("forced target=%d want=%d", got, gid)
	}
	if enterworld.CurrentHP(p.m) != hp {
		t.Fatal("Scorn dealt damage")
	}
	if selected, _ := p.rt.Selected.Get(testDivision, p.m.Name); selected != gid {
		t.Fatalf("Scorn selection=%d want=%d", selected, gid)
	}
	if intent, exists := p.rt.combatIntentFor(testDivision, p.m.Name); !exists || intent.TargetGid != 12345 {
		t.Fatal("Scorn changed a command instead of selection record zero")
	}
	effects := p.rt.effects.Snapshot(testDivision, p.m.Name)
	if len(effects) != 1 || effects[0].ClientCancelable {
		t.Fatalf("recipient effect=%+v", effects)
	}
	at := p.rt.liveSpawn(simulation.WorldKey(testDivision, p.c.Name), p.c, p.clock.NowMs())
	if code := p.rt.skillAdmission(testDivision, p.m, skill, p.clock.NowMs(), &admitTarget{at: at, player: p.c}, nil, admitTargets); code != 0 {
		t.Fatalf("caster target refused: %x", code)
	}
	if code := p.rt.skillAdmission(testDivision, p.m, skill, p.clock.NowMs(), &admitTarget{at: at}, nil, admitTargets); code != 0x3006 {
		t.Fatalf("monster bypassed constraint: %x", code)
	}
	if code := p.rt.skillAdmission(testDivision, p.m, skill, p.clock.NowMs(), &admitTarget{at: at, player: p.m}, nil, admitTargets); code != 0x3006 {
		t.Fatalf("self bypassed constraint: %x", code)
	}
	if got := p.rt.effects.ForcedTarget(testDivision, p.m.Name, effects[0].ExpiresAtMs+1); got != 0 {
		t.Fatal("expired constraint retained", got)
	}
	p.rt.RewardActorPresent = func(_ string, name string) bool { return name != p.c.Name }
	p.rt.advanceLinkedEffects(p.clock.NowMs())
	if got := p.rt.effects.ForcedTarget(testDivision, p.m.Name, p.clock.NowMs()); got != 0 {
		t.Fatal("missing source retained constraint", got)
	}
	if batches := p.rt.drainStoppedCharacterEffects(); len(batches) == 0 || len(p.rt.effects.Snapshot(testDivision, p.m.Name)) != 0 {
		t.Fatal("source loss did not publish and remove the buff instance")
	}
}

/*
================
TestScornDeathAndExpiryOwnership
================
*/
func TestScornDeathAndExpiryOwnership(t *testing.T) {
	for _, mode := range []string{"caster death", "recipient death", "expiry"} {
		t.Run(mode, func(t *testing.T) {
			skill := shippedOffense(t, "SKILL_EU_ROG_STEALTHA_CHANGE_A_01")
			p := scornOpponentPair(t, skill)
			p.cast(skill.ID)
			effects := p.rt.effects.Snapshot(testDivision, p.m.Name)
			if len(effects) != 1 {
				t.Fatal("missing fixture effect")
			}
			switch mode {
			case "caster death":
				p.c.CurrentHP = testInt64(0)
				p.rt.retireBodyEffectsOnDeath(testDivision, p.c)
				p.rt.advanceForcedTargets()
			case "recipient death":
				p.m.CurrentHP = testInt64(0)
				p.rt.retireBodyEffectsOnDeath(testDivision, p.m)
			case "expiry":
				p.rt.effects.Expire(effects[0].ExpiresAtMs)
				if len(p.rt.effects.DrainStopRequested()) != 0 {
					t.Fatal("effect expired at equality")
				}
				p.rt.effects.Expire(effects[0].ExpiresAtMs + 1)
			}
			p.rt.drainStoppedCharacterEffects()
			remaining := len(p.rt.effects.Snapshot(testDivision, p.m.Name))
			if mode == "caster death" && remaining != 1 || mode != "caster death" && remaining != 0 {
				t.Fatalf("remaining effects=%d", remaining)
			}
		})
	}
}

/*
================
TestScornRefusesMonsterWithoutCost
================
*/
func TestScornRefusesMonsterWithoutCost(t *testing.T) {
	rt, _, c, target := newCombatTestRuntime(t, 100000)
	skill := shippedOffense(t, "SKILL_EU_ROG_STEALTHA_CHANGE_A_01")
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 13
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	mp := enterworld.CurrentMP(c)
	out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	if enterworld.CurrentMP(c) != mp || len(rt.effects.ForcedTargets()) != 0 {
		t.Fatalf("monster target changed state: %+v", out)
	}
}

/*
================
TestGrossScornSelectsPrimaryAreaAndLimitsRecipients

Each recipient receives its own effect, up to the authored target count.
================
*/
func TestGrossScornSelectsPrimaryAreaAndLimitsRecipients(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_ROG_STEALTHA_CHANGE_B_01")
	// Keep the level-one stat fixture while isolating the authored area.
	// Resource refusal is exercised separately from recipient selection.
	skill.Consumption.MP = 1
	p := scornOpponentPair(t, skill)
	deps := p.rt.deps.(*enterworld.Deps)
	source := deps.Characters.(enterworld.StaticCharacterSource)
	for i := int64(5); i <= 8; i++ {
		other := *p.m
		other.ID = i
		other.Name = fmt.Sprintf("area-%d", i)
		other.CurrentHP = testInt64(100)
		source[testDivision] = append(source[testDivision], &other)
		position := float64(10)
		if i == 8 {
			position = 1000
		}
		supportPair{rt: p.rt, clock: p.clock, c: p.c, m: &other}.placeMate(position)
	}
	out := p.cast(skill.ID)
	if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok {
		t.Fatalf("Gross Scorn refused: %+v", out)
	}
	var tokens []uint32
	for _, frame := range out.Broadcast {
		if frame.Opcode == wire.OpAttachedEffect {
			tokens = append(tokens, binary.LittleEndian.Uint32(frame.Payload[8:12]))
		}
	}
	effects := p.rt.effects.ForcedTargets()
	if len(effects) != 3 || len(tokens) != 3 {
		t.Fatalf("recipients=%d packets=%d", len(effects), len(tokens))
	}
	seen := make(map[uint32]bool)
	for _, effect := range effects {
		if effect.CharacterName == p.c.Name || effect.CharacterName == "area-8" || seen[effect.InstanceToken] {
			t.Fatalf("invalid recipient %+v", effect)
		}
		seen[effect.InstanceToken] = true
	}
}

/*
================
TestGrossScornSecondaryAdmissionAndRadius

The primary is retained independently. Each additional target must satisfy
world, relation, life and body-expanded radius checks before the count limit.
================
*/
func TestGrossScornSecondaryAdmissionAndRadius(t *testing.T) {
	for _, mode := range []string{"boundary", "outside", "foreign world", "ally", "dead", "absent", "primary only"} {
		t.Run(mode, func(t *testing.T) {
			skill := shippedOffense(t, "SKILL_EU_ROG_STEALTHA_CHANGE_B_01")
			p := scornOpponentPair(t, skill)
			other := p.m.Snapshot()
			other.ID, other.Name = 9, "secondary"
			source := p.rt.deps.(*enterworld.Deps).Characters.(enterworld.StaticCharacterSource)
			source[testDivision] = append(source[testDivision], other)
			casterRadius, _ := p.rt.deps.CharacterBodyRadius(p.c)
			otherRadius, _ := p.rt.deps.CharacterBodyRadius(other)
			distance := float64(skill.TimedEffect.Area.Radius) + casterRadius + otherRadius
			switch mode {
			case "outside":
				distance += 0.01
			case "foreign world":
				world := uint32(0x20001)
				other.World.PackedInstance = &world
			case "ally":
				other.MissionInventory = append([]enterworld.InventoryRow(nil), p.c.MissionInventory...)
			case "dead":
				other.CurrentHP = testInt64(0)
			case "absent":
				p.rt.RewardActorPresent = func(_, name string) bool { return name != other.Name }
			case "primary only":
				skill.TimedEffect.Area.MaxTargets = 1
			}
			supportPair{rt: p.rt, clock: p.clock, c: p.c, m: other}.placeMate(distance)
			got := p.rt.forcedTargetRecipients(tauntPlayerCast{division: testDivision, caster: p.c, snapshot: p.c.Snapshot(), target: p.m, skill: skill, now: p.clock.NowMs()})
			want := 1
			if mode == "boundary" {
				want = 2
			}
			if len(got) != want || got[0].ID != p.m.ID {
				t.Fatalf("recipients=%v want count=%d", got, want)
			}
		})
	}
}

/*
================
TestScornPlayerRefusalsDoNotSpend
================
*/
func TestScornPlayerRefusalsDoNotSpend(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_ROG_STEALTHA_CHANGE_A_01")
	for _, mode := range []string{"town", "missing region", "party", "same cape", "foreign world", "dead", "unlearned", "wrong weapon"} {
		t.Run(mode, func(t *testing.T) {
			p := scornOpponentPair(t, skill)
			switch mode {
			case "town", "missing region":
				region := uint16(0x62a8)
				if mode == "missing region" {
					region = 0
				}
				for _, actor := range []*enterworld.Character{p.c, p.m} {
					p.rt.Worlds.Update(simulation.WorldKey(testDivision, actor.Name), func() simulation.WorldState {
						return simulation.SeedWorldState(actor)
					}, func(w *simulation.WorldState) { w.Spawn.RegionID = region })
				}
			case "party":
				p.rt.RewardParties = func(string) []RewardParty {
					return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(p.c), enterworld.ObjectIDForCharacter(p.m)}}}
				}
			case "same cape":
				items := p.rt.deps.ItemReferences().(staticItemSource)
				cape := items[p.m.MissionInventory[1].Codename]
				cape.NativeFields = cape.NativeFields.With(freeBattleGroupField, 1)
			case "foreign world":
				other := uint32(0x20001)
				p.m.World.PackedInstance = &other
			case "dead":
				p.m.CurrentHP = testInt64(0)
			case "unlearned":
				p.c.Skills = nil
			case "wrong weapon":
				p.rt.deps.ItemReferences().(staticItemSource)[p.c.MissionInventory[0].Codename].TypeIDs[3] = 2
			}
			mp := enterworld.CurrentMP(p.c)
			out := p.cast(skill.ID)
			if len(p.rt.effects.ForcedTargets()) != 0 || enterworld.CurrentMP(p.c) != mp {
				t.Fatalf("refusal changed state: %+v", out)
			}
			if frame, ok := findFrame(out.Frames, wire.OpSkillCastResult); ok && len(frame.Payload) != 0 && frame.Payload[0] == 1 {
				t.Fatal("refusal emitted cast success")
			}
		})
	}
}

/*
================
TestScornWakesSleepAndPublishesAfterOpen

hitm belongs to the execution-selector family even though its hit has no
damage. Its shared hit consequence wakes sleep and preserves health.
================
*/
func TestScornWakesSleepAndPublishesAfterOpen(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_ROG_STEALTHA_CHANGE_A_01")
	p := scornOpponentPair(t, skill)
	seedPlayerStatus(p.rt, p.m, abnormal.Sleep, 10000, p.clock.NowMs(), enterworld.ObjectIDForCharacter(p.c))
	hp := enterworld.CurrentHP(p.m)
	out := p.cast(skill.ID)
	block := p.rt.playerAbnormal(testDivision, p.m.Name)
	if block != nil && block.Mask&abnormal.Sleep.Bit() != 0 {
		t.Fatal("Scorn retained sleep")
	}
	if enterworld.CurrentHP(p.m) != hp {
		t.Fatal("Scorn hit consequence reduced HP")
	}
	opened := false
	for _, frame := range out.Broadcast {
		if frame.Opcode == wire.OpSkillCastResult && len(frame.Payload) != 0 && frame.Payload[0] == 1 {
			opened = true
		}
		if frame.Opcode == wire.OpAttachedEffect && !opened {
			t.Fatal("effect published before its cast opened")
		}
	}
	if !opened {
		t.Fatalf("Scorn did not open: %+v", out)
	}
}
