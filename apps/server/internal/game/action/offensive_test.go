/*
===========================================================================

offensive_test.go - advanced offensive skills: cost, range, refusals, tags

===========================================================================
*/

package action

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
shippedOffense

Resolve authored rows; asset-free environments skip these catalog cases.
================
*/
func shippedOffense(t *testing.T, code string) enterworld.SkillRow {
	t.Helper()
	dir := licensed.RetailTextdataDir(t)
	if _, err := os.Stat(filepath.Join(dir, "skilldata.txt")); err != nil {
		t.Skip("shipped skilldata unavailable")
	}
	row, ok := enterworld.NewTextdataSkills(dir).SkillByCodename(code)
	if !ok {
		t.Fatalf("missing skill %s", code)
	}
	return row
}

/*
================
TestAdvancedOffenseCommitsMPCooldownAndDamageOnce

Preparation owns cooldown; release owns the resource and damage transaction.
================
*/
func TestAdvancedOffenseCommitsMPCooldownAndDamageOnce(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	skill := shippedOffense(t, "SKILL_CH_SWORD_SMASH_A_01")
	if !skill.DirectOffensePinned || skill.Consumption.MP != 19 {
		t.Fatalf("skill not admitted: %+v", skill)
	}
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	mp := int64(19)
	c.CurrentMP = &mp
	cast := wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode()
	acceptedAt := clock.NowMs()
	r := rt.HandleTargetInteract(testDivision, c, cast)
	r = assertAndSeparateActionSession(t, r)
	assertOpcodes(t, r.Frames, wire.OpSkillCastResult)
	if *c.CurrentMP != 19 {
		t.Fatal("preparation charged MP")
	}
	clock.now = clock.At(time.Duration(skill.ActionCastingTimeMs+1) * time.Millisecond)
	r = releasePreparedSkillForTest(t, rt, clock.NowMs())
	assertOpcodes(t, r.Frames, wire.OpSkillEffectControl, simulation.OpVitalsUpdate)
	if *c.CurrentMP != 0 {
		t.Fatal("exact MP not debited")
	}
	after, _ := rt.Monsters.Get(testDivision, target.Gid)
	if after.CurrentHP >= target.CurrentHP {
		t.Fatal("no advanced damage")
	}
	if intents := rt.combatIntentSnapshot(); len(intents) != 1 || !intents[0].ResumeBasic {
		t.Fatal("authored basic continuation missing")
	}
	rt.ClearCombatIntent(testDivision, c.Name)
	if c.OffensiveSkillCooldowns[skill.Group] != acceptedAt+3000 {
		t.Fatal("cooldown not committed")
	}
	// A restart/reconnect cannot clear cooldown; snapshots cannot mutate it.
	clone := rt.characterSnapshot(testDivision, c)
	clone.OffensiveSkillCooldowns[skill.Group] = 0
	if c.OffensiveSkillCooldowns[skill.Group] == 0 {
		t.Fatal("cooldown snapshot alias")
	}
	raw, _ := json.Marshal(c)
	var restored enterworld.Character
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.OffensiveSkillCooldowns[skill.Group] != c.OffensiveSkillCooldowns[skill.Group] {
		t.Fatal("cooldown not durable")
	}
	rt.TickHook()(clock.At(2 * time.Second).UnixMilli())
	mp = 100
	c.CurrentMP = &mp
	refused := rt.HandleTargetInteract(testDivision, c, cast)
	if len(refused.Frames) != 1 || !bytes.Equal(refused.Frames[0].Payload, []byte{2, 5}) {
		t.Fatalf("cooldown refusal %+v", refused)
	}
	repeated, _ := rt.Monsters.Get(testDivision, target.Gid)
	if repeated.CurrentHP != after.CurrentHP || *c.CurrentMP != 100 {
		t.Fatal("cooldown replay changed authority")
	}
	clock.now = clock.At(3 * time.Second)
	rt.TickHook()(clock.NowMs())
	accepted := rt.HandleTargetInteract(testDivision, c, cast)
	if len(accepted.Frames) == 0 || accepted.Frames[0].Payload[0] != 1 || *c.CurrentMP != 100 {
		t.Fatal("exact cooldown deadline refused")
	}
	releasePreparedSkillForTest(t, rt, clock.NowMs()+int64(skill.ActionCastingTimeMs)+1)
	if *c.CurrentMP != 81 {
		t.Fatal("second release cost")
	}

}

/*
================
TestAdvancedSpellUsesAuthoredRangeAndUnrestrictedWeapon
================
*/
func TestAdvancedSpellUsesAuthoredRangeAndUnrestrictedWeapon(t *testing.T) {
	rt, _, c, target := newCombatTestRuntime(t, 100000)
	skill := shippedOffense(t, "SKILL_CH_FIRE_GIGONGSUL_A_01")
	if !skill.DirectOffensePinned || skill.ActionRange != 150 {
		t.Fatalf("fire spell shape %+v", skill)
	}
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.Intellect = testInt64(200)
	mp := int64(1000)
	c.CurrentMP = &mp
	// 100 units away: beyond sword reach, inside the authored spell range.
	x := target.Spawn.X - 100
	c.World.Spawn.X = &x
	r := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	if len(r.Frames) == 0 || r.Frames[0].Opcode != wire.OpSkillCastResult {
		t.Fatalf("spell refused: %+v", r)
	}
	releasePreparedSkillForTest(t, rt, rt.Now().UnixMilli()+int64(skill.ActionCastingTimeMs)+1)
	if *c.CurrentMP != 652 {
		t.Fatalf("spell MP=%d", *c.CurrentMP)
	}
	if len(rt.combatIntentSnapshot()) != 0 {
		t.Fatal("in-range spell pursued/repeated")
	}
}

/*
================
TestAdvancedRefusalDoesNotSpendOrDamage

Incomplete descriptors and failed admission cannot reach authority mutation.
================
*/
func TestAdvancedRefusalDoesNotSpendOrDamage(t *testing.T) {
	for _, mode := range []string{"short-mp", "rng-failure", "unsupported-effect", "unlearned"} {
		t.Run(mode, func(t *testing.T) {
			rt, _, c, target := newCombatTestRuntime(t, 100000)
			skill := shippedOffense(t, "SKILL_CH_SWORD_SMASH_A_01")
			mp := int64(100)
			if mode == "short-mp" {
				mp = 18
			}
			c.CurrentMP = &mp
			before := mp
			if mode == "unsupported-effect" {
				// Exercise the authority contract independently of which retail
				// families have been implemented. Parameter presence is not
				// permission to execute an uncompiled program.
				skill.DirectOffensePinned = false
				skill.OffensiveStagePinned = false
			}
			rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
			if mode != "unlearned" {
				c.Skills = append(c.Skills, skill.ID)
			}
			if mode == "rng-failure" {
				rt.CombatRoll = func() (uint32, error) { return 0, errors.New("no random source") }
			}
			r := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
			if mode == "short-mp" && (len(r.Frames) != 1 || !bytes.Equal(r.Frames[0].Payload, []byte{2, 4})) {
				t.Fatalf("wrong native MP refusal %+v", r)
			}
			expectedTokens := uint32(0)
			if mode == "rng-failure" {
				expectedTokens = 1
				releasePreparedSkillForTest(t, rt, rt.Now().UnixMilli()+int64(skill.ActionCastingTimeMs)+1)
			}
			after, _ := rt.Monsters.Get(testDivision, target.Gid)
			if after.CurrentHP != target.CurrentHP || *c.CurrentMP != before || len(c.OffensiveSkillCooldowns) != int(expectedTokens) || rt.castTokenCounter != expectedTokens {
				t.Fatal("refused cast changed authority")
			}
		})
	}
}

/*
================
TestAdvancedPursuitCastsOnceAndCanBeCancelled
================
*/
func TestAdvancedPursuitCastsOnceAndCanBeCancelled(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprint(cancel), func(t *testing.T) {
			rt, clock, c, target := newCombatTestRuntime(t, 100000)
			skill := shippedOffense(t, "SKILL_CH_SWORD_SMASH_A_01")
			rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
			c.Skills = append(c.Skills, skill.ID)
			c.CurrentMP = testInt64(100)
			*c.World.Spawn.X = 900
			result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
			result = assertAndSeparateActionSession(t, result)
			if len(result.Frames) != 1 || result.Frames[0].Opcode != simulation.OpMovementAck {
				t.Fatalf("no pursuit: %+v", result)
			}
			if *c.CurrentMP != 100 || rt.castTokenCounter != 0 {
				t.Fatal("pursuit spent before range admission")
			}
			if cancel {
				rt.ClearCombatIntent(testDivision, c.Name)
			}
			privateVitals := 0
			for tick := 1; tick <= 100; tick++ {
				for _, burst := range rt.TickHook()(clock.At(time.Duration(tick) * 100 * time.Millisecond).UnixMilli()) {
					for _, frame := range burst.Frames {
						if frame.Opcode == simulation.OpVitalsUpdate {
							if burst.OnlyCharacterID != c.ID {
								t.Fatal("MP leaked to public route")
							}
							privateVitals++
						}
					}
				}
			}
			if cancel {
				after, _ := rt.Monsters.Get(testDivision, target.Gid)
				if *c.CurrentMP != 100 || after.CurrentHP != target.CurrentHP || rt.castTokenCounter != 0 {
					t.Fatal("cancelled pursuit cast")
				}
			} else if *c.CurrentMP != 81 || rt.castTokenCounter < 2 || privateVitals != 1 {
				t.Fatalf("one-shot MP=%d tokens=%d private=%d", *c.CurrentMP, rt.castTokenCounter, privateVitals)
			}
			intents := rt.combatIntentSnapshot()
			if cancel && len(intents) != 0 || !cancel && (len(intents) != 1 || intents[0].SingleCast || intents[0].SkillID != 2) {
				t.Fatal("wrong continuation owner", intents)
			}
		})
	}
}

/*
================
TestAdvancedPercentageCostUsesMaximumAndFloorsBeforeAdmission
================
*/
func TestAdvancedPercentageCostUsesMaximumAndFloorsBeforeAdmission(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100)
	skill := enterworld.SkillRow{Group: 7, CoolTimeMs: 3000, Consumption: enterworld.SkillConsumption{MP: 3, MPPercent: 7, Pinned: true}}
	max := enterworld.DerivedMaxMP(c)
	expected := int64(3) + max*7/100
	c.CurrentMP = testInt64(expected)
	cost, code := rt.offensiveCost(testDivision, c, skill, 100)
	if code != 0 || cost != expected {
		t.Fatalf("exact percent cost %d/%x want %d", cost, code, expected)
	}
	c.CurrentMP = testInt64(expected - 1)
	if _, code := rt.offensiveCost(testDivision, c, skill, 100); code != 0x3004 {
		t.Fatal("percentage shortage admitted")
	}
	c.CurrentMP = testInt64(expected)
	c.OffensiveSkillCooldowns = map[uint32]int64{2: 99, 3: 101}
	rt.commitOffensiveCost(testDivision, c, skill, skillCharge{mp: cost}, 100)
	if *c.CurrentMP != 0 || c.OffensiveSkillCooldowns[7] != 3100 || c.OffensiveSkillCooldowns[3] != 101 {
		t.Fatal("wrong cost/cooldown commit")
	}
	if _, exists := c.OffensiveSkillCooldowns[2]; exists {
		t.Fatal("expired cooldown retained")
	}
}

/*
================
TestShippedPierceCarriesAtca
================
*/
func TestShippedPierceCarriesAtca(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_WARRIOR_ONEHANDA_PIERCE_A_01")
	if skill.OffenseRefusal != "" || !skill.Attack.Atca || skill.Attack.AtcaMask != 16832 || skill.Attack.AtcaPercent != 50 {
		t.Fatalf("atca row %+v %s", skill.Attack, skill.OffenseRefusal)
	}
}

/*
================
TestShippedMPDecreaseKeysArePinned
================
*/
func TestShippedMPDecreaseKeysArePinned(t *testing.T) {
	wizard := shippedOffense(t, "SKILL_EU_WIZARD_EARTHA_POINT_A_01")
	if !wizard.DirectOffensePinned || !wizard.Attack.Parameters.Has(enterworld.ParameterWizardMPDecrease) {
		t.Fatalf("WIMD row %+v %s", wizard.Attack.Parameters, wizard.OffenseRefusal)
	}
	bard := shippedOffense(t, "SKILL_EU_BARD_BATTLAA_DAMAGE_A_01")
	if !bard.DirectOffensePinned || !bard.Attack.Parameters.Has(enterworld.ParameterBardMPDecrease) {
		t.Fatalf("BDMD row %+v %s", bard.Attack.Parameters, bard.OffenseRefusal)
	}
}

/*
================
TestReqiShieldRequiresSecondaryTID4
================
*/
func TestReqiShieldRequiresSecondaryTID4(t *testing.T) {
	rt, _, c, target := newCombatTestRuntime(t, 100000)
	skill := shippedOffense(t, "SKILL_EU_WARRIOR_ONEHANDA_SHIELD_A_01")
	if !skill.DirectOffensePinned || !skill.Reqi.Present || skill.Reqi.Pairs[0] != (enterworld.SkillReqiPair{Kind: 4, Value: 2}) {
		t.Fatalf("reqi row %+v %s", skill.Reqi, skill.OffenseRefusal)
	}
	skill.RequiredWeaponKinds = [2]uint8{0xff, 0xff}
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	mp := int64(500)
	c.CurrentMP = &mp
	r := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	if len(r.Frames) != 1 || !bytes.Equal(r.Frames[0].Payload, []byte{2, 0x0d}) || *c.CurrentMP != 500 {
		t.Fatalf("bare hand %+v mp %d", r, *c.CurrentMP)
	}
}

/*
================
TestReqcDownAttackRequiresMotion8
================
*/
func TestReqcDownAttackRequiresMotion8(t *testing.T) {
	for _, downed := range []bool{false, true} {
		t.Run(fmt.Sprint(downed), func(t *testing.T) {
			rt, clock, c, target := newCombatTestRuntime(t, 100000)
			skill := shippedOffense(t, "SKILL_CH_SWORD_DOWNATTACK_A_01")
			if !skill.DirectOffensePinned || !skill.Reqc.KnockedDown || skill.Attack.DownAttack.Percent != 125 {
				t.Fatalf("reqc row not admitted: %+v %s", skill.Reqc, skill.OffenseRefusal)
			}
			rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
			c.Skills = append(c.Skills, skill.ID)
			before := int64(100)
			c.CurrentMP = &before
			now := clock.NowMs()
			if downed {
				mover, ok := rt.Monsters.Mover(testDivision, target.Gid)
				if !ok {
					t.Fatal("missing mover")
				}
				rt.Monsters.ApplyDamageSequence(testDivision, target.Gid, target.CurrentHP, []simulation.MonsterDamagePlan{{
					GID: target.Gid, Knockdown: &simulation.MonsterKnockdownPlan{Pose: mover.Pose, UntilMs: now + 10000},
				}})
			}
			r := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
			if !downed {
				if len(r.Frames) != 1 || !bytes.Equal(r.Frames[0].Payload, []byte{2, 6}) || *c.CurrentMP != before {
					t.Fatalf("standing target %+v mp %d", r, *c.CurrentMP)
				}
				after, _ := rt.Monsters.Get(testDivision, target.Gid)
				if after.CurrentHP != target.CurrentHP {
					t.Fatal("standing target took damage")
				}
				return
			}
			if len(r.Frames) == 0 || r.Frames[0].Opcode != wire.OpSkillCastResult || r.Frames[0].Payload[0] != 1 {
				t.Fatalf("downed target refused: %+v", r)
			}
			releasePreparedSkillForTest(t, rt, now+int64(skill.ActionCastingTimeMs)+1)
			after, _ := rt.Monsters.Get(testDivision, target.Gid)
			if after.CurrentHP >= target.CurrentHP || *c.CurrentMP == before {
				t.Fatalf("downed target hp %d mp %d", after.CurrentHP, *c.CurrentMP)
			}
		})
	}
}

// Advance the actual preparation owner. Return its public and actor-private
// frames without changing opcodes or synthesizing a successful cast result.
/*
================
releasePreparedSkillForTest

Advance the real preparation owner and collect its public/private results.
================
*/
func releasePreparedSkillForTest(t *testing.T, rt *Runtime, now int64) OpResult {
	t.Helper()
	batches := rt.advanceProjectileCasts(now)
	if len(batches) == 0 {
		t.Fatal("prepared skill did not release or refuse")
	}
	var out OpResult
	for _, batch := range batches {
		for _, frame := range batch.Frames {
			f := wire.Frame{Opcode: frame.Opcode, Payload: frame.Payload}
			out.Frames = append(out.Frames, f)
			if batch.OnlyCharacterID == 0 {
				out.Broadcast = append(out.Broadcast, f)
			} else {
				out.ActorPrivate = append(out.ActorPrivate, f)
			}
		}
	}
	return out
}

/*
================
installMPDecrease

Install a learned modifier through the same catalog used by cast costs.
================
*/
func installMPDecrease(rt *Runtime, c *enterworld.Character, slot enterworld.SkillParameter) {
	id := uint32(910000) + uint32(slot)
	row := enterworld.SkillRow{ID: id, Group: id, Level: 1, PassiveParameters: enterworld.SkillPassiveParameters{Pinned: true}}
	row.PassiveParameters.Mask = enterworld.SkillParameterMask(1) << slot
	row.PassiveParameters.Values[slot] = 50
	rt.deps.SkillData().(staticSkillSource)[id] = row
	c.Skills = append(c.Skills, id)
}

/*
================
mpDecreaseOutcome

Measure committed resources and HP rather than inspecting compiler flags.
================
*/
func mpDecreaseOutcome(t *testing.T, code string, slot enterworld.SkillParameter, cut bool) (spent int64, lost uint32) {
	t.Helper()
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	skill := shippedOffense(t, code)
	if !skill.Attack.Parameters.Has(slot) || skill.ProjectileSpeed != 0 {
		t.Fatalf("row %s refusal %s", code, skill.OffenseRefusal)
	}
	skill.RequiredWeaponKinds = [2]uint8{0xff, 0xff}
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	if cut {
		installMPDecrease(rt, c, slot)
	}
	mp := int64(50000)
	c.CurrentMP = &mp
	_, _, _, beforeMP := rt.playerKeeperVitals(testDivision, c)
	prepared, err := rt.preparedExecutionMPCost(testDivision, c, skill)
	if err != nil {
		t.Fatal(err)
	}
	if !skill.DirectOffensePinned {
		return prepared, 0
	}
	r := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	if len(r.Frames) == 0 || r.Frames[0].Opcode != wire.OpSkillCastResult || r.Frames[0].Payload[0] != 1 {
		t.Fatalf("cast refused %+v", r)
	}
	if skill.ActionCastingTimeMs > 0 {
		releasePreparedSkillForTest(t, rt, clock.NowMs()+int64(skill.ActionCastingTimeMs)+1)
	}
	_, _, _, afterMP := rt.playerKeeperVitals(testDivision, c)
	after, _ := rt.Monsters.Get(testDivision, target.Gid)
	if beforeMP-afterMP != prepared {
		t.Fatalf("%s charged %d, prepared %d", code, beforeMP-afterMP, prepared)
	}
	return prepared, target.CurrentHP - after.CurrentHP
}

/*
================
TestMPDecreaseCutsPreparedCostNotDamage
================
*/
func TestMPDecreaseCutsPreparedCostNotDamage(t *testing.T) {
	for _, tc := range []struct {
		code string
		slot enterworld.SkillParameter
	}{
		{"SKILL_EU_WIZARD_EARTHA_POINT_A_01", enterworld.ParameterWizardMPDecrease},
		{"SKILL_EU_BARD_BATTLAA_DAMAGE_A_01", enterworld.ParameterBardMPDecrease},
		{"SKILL_EU_CLERIC_HEALA_TARGET_A_01", enterworld.ParameterHealerMPDecrease},
	} {
		t.Run(tc.code, func(t *testing.T) {
			plainSpent, plainLost := mpDecreaseOutcome(t, tc.code, tc.slot, false)
			cutSpent, cutLost := mpDecreaseOutcome(t, tc.code, tc.slot, true)
			if plainSpent == 0 || cutSpent != plainSpent/2 || cutLost != plainLost {
				t.Fatalf("spent %d -> %d, damage %d -> %d", plainSpent, cutSpent, plainLost, cutLost)
			}
		})
	}
}

/*
================
TestAtcaUsesLiveAbnormalMask
================
*/
func TestAtcaUsesLiveAbnormalMask(t *testing.T) {
	var lost [2]uint32
	for i, stun := range []bool{false, true} {
		rt, clock, c, target := newCombatTestRuntime(t, 100000)
		rt.CombatRoll = func() (uint32, error) { return 0, nil }
		skill := shippedOffense(t, "SKILL_EU_WARRIOR_ONEHANDA_PIERCE_A_01")
		if skill.OffenseRefusal != "" || !skill.Attack.Atca || skill.Attack.AtcaMask != 0x41c0 || skill.Attack.AtcaPercent != 50 {
			t.Fatalf("atca row %+v %s", skill.Attack, skill.OffenseRefusal)
		}
		skill.RequiredWeaponKinds = [2]uint8{0xff, 0xff}
		skill.ChainNext = 0
		rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
		c.Skills = append(c.Skills, skill.ID)
		mp := int64(50000)
		c.CurrentMP = &mp
		if stun {
			records := []abnormal.Record{{Status: abnormal.Stun, DurationMs: 10000, Level: 1, SourceGID: enterworld.ObjectIDForCharacter(c), SourceName: c.Name}}
			applied := rt.Monsters.ApplyDamageSequence(testDivision, target.Gid, target.CurrentHP, []simulation.MonsterDamagePlan{{
				GID: target.Gid, Abnormal: records, AbnormalSources: rt.Monsters.PrepareAbnormalSources(testDivision, records),
			}})
			if len(applied) != 1 || applied[0].Instance.AbnormalMask()&abnormal.Stun.Bit() == 0 {
				t.Fatal("stun was not installed")
			}
		}
		r := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
		if len(r.Frames) == 0 || r.Frames[0].Payload[0] != 1 {
			t.Fatalf("stun=%v refused %+v", stun, r)
		}
		if skill.ActionCastingTimeMs > 0 {
			releasePreparedSkillForTest(t, rt, clock.NowMs()+int64(skill.ActionCastingTimeMs)+1)
		}
		after, _ := rt.Monsters.Get(testDivision, target.Gid)
		lost[i] = target.CurrentHP - after.CurrentHP
	}
	if lost[0] == 0 || lost[1] != lost[0]+lost[0]/2 {
		t.Fatalf("unstunned %d stunned %d", lost[0], lost[1])
	}
}

/*
================
TestReqcDanceRequiresSelectorBit
================
*/
func TestReqcDanceRequiresSelectorBit(t *testing.T) {
	guard := shippedOffense(t, "SKILL_EU_BARD_BATTLAA_GUARD_A_01")
	if guard.SelectorMask != 1 {
		t.Fatalf("scls mask %d", guard.SelectorMask)
	}
	for _, dancing := range []bool{false, true} {
		rt, _, c, target := newCombatTestRuntime(t, 100000)
		skill := shippedOffense(t, "SKILL_CH_SWORD_DOWNATTACK_A_01")
		skill.Reqc = enterworld.SkillReqc{Present: true, Dance: true}
		skill.RequiredWeaponKinds = [2]uint8{0xff, 0xff}
		src := rt.deps.SkillData().(staticSkillSource)
		src[skill.ID] = skill
		src[guard.ID] = guard
		c.Skills = append(c.Skills, skill.ID)
		mp := int64(50000)
		c.CurrentMP = &mp
		if dancing {
			// Another Bard's Guard Tambour: a child instance naming its
			// caster's instance (reqc 32 needs another Bard's music).
			if !rt.effects.Apply(statuseffect.Effect{DivisionID: testDivision, CharacterName: c.Name, SkillID: guard.ID, SkillGroup: guard.Group, AuraParentToken: 1}) {
				t.Fatal("guard effect refused")
			}
		}
		r := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
		if !dancing {
			if len(r.Frames) != 1 || !bytes.Equal(r.Frames[0].Payload, []byte{2, 0x32}) || *c.CurrentMP != 50000 {
				t.Fatalf("no dance %+v mp %d", r, *c.CurrentMP)
			}
			continue
		}
		if len(r.Frames) == 0 || r.Frames[0].Payload[0] != 1 {
			t.Fatalf("dance refused %+v", r)
		}
	}
}

/*
==================
TestShippedActionHandlerKinds

Column 68 against the handler each row must run: a crossbow base attack
is a projectile at flying speed 0, the monster mask persists, a wizard
point strike is instant.
==================
*/
func TestShippedActionHandlerKinds(t *testing.T) {
	for code, want := range map[string]enterworld.SkillActionHandler{
		"SKILL_EU_CROSSBOW_BASE_01":         enterworld.SkillActionProjectile,
		"SKILL_CH_SPEAR_SHOOT_A_01":         enterworld.SkillActionProjectile,
		"SKILL_ETC_TRANS_MONSTER_01":        enterworld.SkillActionPersistent,
		"SKILL_EU_WIZARD_EARTHA_POINT_A_01": enterworld.SkillActionInstant,
	} {
		if row := shippedOffense(t, code); row.ActionHandler != want {
			t.Errorf("%s handler %d, want %d", code, row.ActionHandler, want)
		}
	}
	if row := shippedOffense(t, "SKILL_EU_CROSSBOW_BASE_01"); row.ProjectileSpeed != 0 {
		t.Fatalf("crossbow base flies at %d; the zero-speed projectile case is gone", row.ProjectileSpeed)
	}
}

/*
==================
TestProjectileHandlerSkipsMPDecrease

The cut follows the handler, not the flying speed: the same row at speed
0 loses the cut once it runs SkillAction_Projectile.
==================
*/
func TestProjectileHandlerSkipsMPDecrease(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100000)
	skill := shippedOffense(t, "SKILL_EU_WIZARD_EARTHA_POINT_A_01")
	installMPDecrease(rt, c, enterworld.ParameterWizardMPDecrease)
	mp := int64(50000)
	c.CurrentMP = &mp

	instant, err := rt.preparedExecutionMPCost(testDivision, c, skill)
	if err != nil {
		t.Fatal(err)
	}
	skill.ActionHandler = enterworld.SkillActionProjectile
	projectile, err := rt.preparedExecutionMPCost(testDivision, c, skill)
	if err != nil {
		t.Fatal(err)
	}
	if skill.ProjectileSpeed != 0 || projectile == 0 || instant != projectile/2 {
		t.Fatalf("instant %d, projectile %d", instant, projectile)
	}
}
