/*
===========================================================================

frenzy_test.go - Warrior Frenzy through the production skill and effect owners

Authored rows must produce real keeper changes and retire them with their
recipient effect. The fixture preserves the level-one stat catalog contract.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"strings"
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
frenzyFixture
================
*/
func frenzyFixture(t *testing.T, code string) (*Runtime, *fakeClock, *enterworld.Character, enterworld.SkillRow, uint32) {
	t.Helper()
	rt, clock, c, target := newCombatTestRuntime(t, 1000000)
	skill := shippedOffense(t, "SKILL_EU_WARRIOR_FRENZYA_"+code+"_01")
	c.RaceIndex = testInt64(enterworld.RaceEurope)
	c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	c.Skills = []uint32{skill.ID}
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(10000)
	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 8
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	return rt, clock, c, skill, target.Gid
}

/*
================
TestFrenzyAttributeCatalog
================
*/
func TestFrenzyAttributeCatalog(t *testing.T) {
	source := shippedSkillSource(t)
	count := 0
	for id := uint32(1); id < 65536; id++ {
		row, ok := source.SkillByID(id)
		if !ok || row.ChainSub || !(strings.HasPrefix(row.Codename, "SKILL_EU_WARRIOR_FRENZYA_HEALTH_") ||
			strings.HasPrefix(row.Codename, "SKILL_EU_WARRIOR_FRENZYA_DAMAGE_")) {
			continue
		}
		count++
		if !row.TimedEffect.Pinned {
			t.Errorf("%s lacks complete timed producer", row.Codename)
		}
	}
	if count != 26 {
		t.Fatalf("catalog has %d attribute ranks, want 26", count)
	}
}

/*
================
TestFrenzyAttributeReleaseAndRetirement

The HP buff does not heal on installation. Expiry clamps a subsequently filled
gauge and restores both damage lanes; the attack buff changes both bounds.
================
*/
func TestFrenzyAttributeReleaseAndRetirement(t *testing.T) {
	for _, variant := range []string{"HEALTH_A", "DAMAGE_A"} {
		t.Run(variant, func(t *testing.T) {
			rt, clock, c, skill, _ := frenzyFixture(t, variant)
			base, _, err := rt.playerCombatStats(testDivision, c)
			if err != nil {
				t.Fatal(err)
			}
			hp, mp := enterworld.CurrentHP(c), enterworld.CurrentMP(c)
			out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
			out = assertAndSeparateActionSession(t, out)
			assertOpcodes(t, out.Frames, wire.OpSkillCastResult)
			if len(rt.effects.Snapshot(testDivision, c.Name)) != 0 || enterworld.CurrentMP(c) != mp {
				t.Fatal("preparation installed or charged")
			}
			released := clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1
			rt.advanceProjectileCasts(released)
			buffed, _, err := rt.playerCombatStats(testDivision, c)
			if err != nil || len(rt.effects.Snapshot(testDivision, c.Name)) != 1 || enterworld.CurrentHP(c) != hp ||
				enterworld.CurrentMP(c) != mp-int64(skill.Consumption.MP) {
				t.Fatal("release failed", err, out)
			}
			for _, parameter := range []uint16{3, 0x0d, 0x0e, 0xb2, 0xb3} {
				before, _ := base.Param(parameter)
				after, _ := buffed.Param(parameter)
				want := before
				if variant == "HEALTH_A" {
					if parameter == 3 {
						want += 583
					}
					if parameter == 0xb2 || parameter == 0xb3 {
						want = 0.65
					}
				} else if parameter == 0x0d || parameter == 0x0e {
					want += 6
				}
				if after != want {
					t.Errorf("parameter %x = %v, want %v (base %v)", parameter, after, want, before)
				}
			}
			if variant == "HEALTH_A" {
				maximum, _ := buffed.Param(3)
				c.CurrentHP = testInt64(int64(maximum))
			}
			rt.effects.Expire(released + int64(skill.EffectDurationMs) + 1)
			rt.drainStoppedCharacterEffects()
			after, _, err := rt.playerCombatStats(testDivision, c)
			if err != nil {
				t.Fatal(err)
			}
			if variant == "HEALTH_A" {
				maximum, _ := base.Param(3)
				if enterworld.CurrentHP(c) != int64(maximum) {
					t.Fatalf("expiry did not clamp HP: %d, want %v", enterworld.CurrentHP(c), maximum)
				}
			}
			for _, parameter := range []uint16{3, 0x0d, 0x0e, 0xb2, 0xb3} {
				want, _ := base.Param(parameter)
				got, _ := after.Param(parameter)
				if got != want {
					t.Errorf("retired parameter %x = %v, want %v", parameter, got, want)
				}
			}
		})
	}
}

/*
================
TestFrenzyTauntAndChargeCatalog

Every authored rank must qualify without fabricating an attack for taunts or
reclassifying a target charge as a ground-targeted movement command.
================
*/
func TestFrenzyTauntAndChargeCatalog(t *testing.T) {
	source := shippedSkillSource(t)
	taunts, charges := 0, 0
	for id := uint32(1); id < 65536; id++ {
		row, ok := source.SkillByID(id)
		if !ok || row.ChainSub || !strings.HasPrefix(row.Codename, "SKILL_EU_WARRIOR_FRENZYA_TOUNT_") {
			continue
		}
		if strings.Contains(row.Codename, "SPRINT") {
			charges++
			_, executable := enterworld.OffensiveSequence(source, id)
			if !executable || !row.PositionEffect.Charge || row.PositionEffect.Pinned {
				t.Errorf("charge %s: %s %+v", row.Codename, row.OffenseRefusal, row.PositionEffect)
			}
		} else {
			taunts++
			if !row.Threat.Only || row.Attack.Present || row.DirectOffensePinned {
				t.Errorf("taunt %s: %+v", row.Codename, row.Threat)
			}
		}
	}
	if taunts != 20 || charges != 16 {
		t.Fatalf("catalog taunts=%d charges=%d", taunts, charges)
	}
}

/*
================
TestFrenzyTauntsChangeAggressionWithoutDamage

Both target modes debit their authored resource mix and open/close one action.
The opponent ledger gains aggression but no HP damage or contribution credit.
================
*/
func TestFrenzyTauntsChangeAggressionWithoutDamage(t *testing.T) {
	for _, variant := range []string{"TOUNT_A", "TOUNT_AREA_A"} {
		t.Run(variant, func(t *testing.T) {
			rt, clock, c, skill, gid := frenzyFixture(t, variant)
			before, _ := rt.Monsters.Get(testDivision, gid)
			mp := enterworld.CurrentMP(c)
			request := wire.SkillAction{ActionId: skill.ID}
			if skill.TargetRequired {
				request.HasTarget, request.TargetGid = true, gid
			}
			out := rt.HandleTargetInteract(testDivision, c, request.Encode())
			frame, ok := findFrame(out.Frames, wire.OpSkillCastResult)
			if !ok || len(frame.Payload) < 21 || frame.Payload[0] != 1 {
				t.Fatalf("taunt refused: %+v", out)
			}
			after, _ := rt.Monsters.Get(testDivision, gid)
			if after.CurrentHP != before.CurrentHP || after.Opponents[0].Damage != 0 ||
				after.Opponents[0].Aggression < int32(skill.Threat.Flat) || after.Opponents[0].GID != enterworld.ObjectIDForCharacter(c) {
				t.Fatalf("taunt outcome HP %d->%d, opponents %+v", before.CurrentHP, after.CurrentHP, after.Opponents)
			}
			if enterworld.CurrentMP(c) != mp-int64(skill.Consumption.MP) {
				t.Fatal("MP debit", enterworld.CurrentMP(c))
			}
			rt.drainSkillFinalizes(clock.NowMs() + int64(skill.ActionDurationMs))
			if rt.hasOpenSkillCast(testDivision, c.Name) {
				t.Fatal("taunt retained action ownership")
			}
		})
	}
}

/*
================
TestFrenzyChargeCommitsTravelAndStatusWithoutAuthorityReentry

Force the authored stun to exercise the dangerous source-lookup boundary.
Refused navigation must preserve HP, MP and the caster's position.
================
*/
func TestFrenzyChargeCommitsTravelAndStatusWithoutAuthorityReentry(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		rt, clock, c, skill, gid := frenzyFixture(t, "TOUNT_SPRINT_A")
		index, _ := abnormal.SourceIndex(0x7374)
		skill.Abnormal.Params[index].Args[1] = 100
		rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
		writing := false
		guard := &periodicSourceGuard{MonsterAbnormalContext: monsterAbnormalContext{rt}, t: t, writing: &writing}
		rt.Monsters.SetAbnormalContext(guard)
		rt.deps.(*enterworld.Deps).UpdateCharacters = func(_ []*enterworld.Character, _ string, update func() bool) bool {
			writing = true
			defer func() { writing = false }()
			return update()
		}
		rt.CombatRoll = func() (uint32, error) { return 10, nil }
		key := simulation.WorldKey(testDivision, c.Name)
		rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) {
			w.Spawn.X -= 30
			w.SpawnSet = true
		})
		from := rt.liveSpawn(key, c, clock.NowMs())
		before, _ := rt.Monsters.Get(testDivision, gid)
		mp := enterworld.CurrentMP(c)
		rt.ConstrainMovement = func(_ string, _, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) {
			if blocked {
				return from, &simulation.MoveError{}
			}
			return to, nil
		}
		out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: gid}.Encode())
		after, _ := rt.Monsters.Get(testDivision, gid)
		to := rt.liveSpawn(key, c, clock.NowMs())
		if blocked {
			if after.CurrentHP != before.CurrentHP || enterworld.CurrentMP(c) != mp || to != from {
				t.Fatal("refused charge mutated authority")
			}
			continue
		}
		frame, ok := findFrame(out.Frames, wire.OpSkillCastResult)
		if !ok || len(frame.Payload) < 19 || frame.Payload[18] != 9 {
			t.Fatalf("charge lacks hit and travel: %+v", out)
		}
		if after.CurrentHP >= before.CurrentHP || enterworld.CurrentMP(c) != mp-int64(skill.Consumption.MP) || to == from {
			t.Fatalf("charge outcome HP %d->%d MP %d->%d pose %+v -> %+v", before.CurrentHP, after.CurrentHP, mp, enterworld.CurrentMP(c), from, to)
		}
		if guard.lookups == 0 || after.Abnormal == nil || !after.Abnormal.Slots[abnormal.Stun].Active {
			t.Fatal("charge did not exercise stun source admission")
		}
		token := binary.LittleEndian.Uint32(frame.Payload[10:])
		for _, batch := range rt.drainSkillFinalizes(clock.NowMs() + 1000) {
			for _, f := range batch.Frames {
				if f.Opcode == wire.OpSkillEffectControl && len(f.Payload) == 6 && binary.LittleEndian.Uint32(f.Payload[2:]) == token {
					t.Fatal("server prematurely finalized guided arrival")
				}
			}
		}
	}
}
