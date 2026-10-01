/*
===========================================================================

skilltimedeffect_test.go - timed skill preparation, installation and retirement tests

Exercise the production action owner and its native packet lifecycle.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"testing"
	"time"
)

/*
================
TestPersistentDefenseProducerReconnectAndRetirement
================
*/
func TestPersistentDefenseProducerReconnectAndRetirement(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	skill := shippedOffense(t, "SKILL_CH_COLD_GANGGI_A_01")
	// Exercise the shared native cbuf+dura branch using the already qualified
	// defp producer. This synthetic combination is not a claimed shipped row.
	skill.TimedEffect.Persistent = true
	skill.Replacement.Cbuf = true
	skill.EffectDurationMs = 10000
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	base, err := rt.PlayerBaseStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	if !rt.ApplyCharacterEffect(testDivision, c.Name, skill.ID, 1001, statuseffect.StateActive, false) {
		t.Fatal("persistent defense producer refused")
	}
	if len(c.TimedSkillJobs) != 1 {
		t.Fatal("non-movement producer did not create job")
	}
	buffed, err := rt.PlayerBaseStats(testDivision, c)
	if err != nil || buffed.PhysicalDefense <= base.PhysicalDefense {
		t.Fatal(buffed, err)
	}
	clock.Advance(3 * time.Second)
	rt.ForgetCharacter(testDivision, c.Name)
	if c.TimedSkillJobs[0].RemainingMs != 7000 {
		t.Fatal(c.TimedSkillJobs)
	}
	clock.Advance(time.Hour)
	rt.RestoreTimedSkillJobs(testDivision, c.Name)
	if len(c.TimedSkillJobs) != 1 || c.TimedSkillJobs[0].Token == 1001 {
		t.Fatal(c.TimedSkillJobs)
	}
	restored, err := rt.PlayerBaseStats(testDivision, c)
	if err != nil || restored != buffed {
		t.Fatal("restored contribution mismatch", restored, buffed, err)
	}
	token := c.TimedSkillJobs[0].Token
	rt.RestoreTimedSkillJobs(testDivision, c.Name)
	if c.TimedSkillJobs[0].Token != token {
		t.Fatal("duplicate admission renewed job")
	}
	rt.retireBodyEffectsOnDeath(testDivision, c)
	if len(rt.effects.Snapshot(testDivision, c.Name)) != 1 {
		t.Fatal("continuous effect died")
	}
	clock.Advance(7 * time.Second)
	rt.effects.Expire(clock.NowMs())
	rt.drainStoppedCharacterEffects()
	after, err := rt.PlayerBaseStats(testDivision, c)
	if err != nil || after != base || len(c.TimedSkillJobs) != 0 {
		t.Fatal("retirement did not close job and modifier", after, err, c.TimedSkillJobs)
	}
}

/*
================
TestTimedDefenseCompletePrepareReleaseRetirement
================
*/
func TestTimedDefenseCompletePrepareReleaseRetirement(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	skill := shippedOffense(t, "SKILL_CH_COLD_GANGGI_A_01")
	if !skill.TimedEffect.Pinned {
		t.Fatal("complete duration/defense program not admitted")
	}
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	mpBefore := enterworld.DerivedMaxMP(c)
	c.CurrentMP = testInt64(mpBefore)
	base, err := rt.PlayerBaseStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	start := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
	start = assertAndSeparateActionSession(t, start)
	assertOpcodes(t, start.Frames, wire.OpSkillCastResult)
	castToken := binary.LittleEndian.Uint32(start.Frames[0].Payload[10:])
	if len(rt.effects.Snapshot(testDivision, c.Name)) != 0 || *c.CurrentMP != mpBefore {
		t.Fatal("preparation installed or spent")
	}
	deadline := clock.NowMs() + int64(skill.ActionCastingTimeMs)
	if len(rt.advanceProjectileCasts(deadline)) != 0 {
		t.Fatal("released at equality")
	}
	out := rt.advanceProjectileCasts(deadline + 1)
	if len(out) != 2 || len(out[0].Frames) != 2 || out[0].Frames[0].Opcode != wire.OpSkillEffectControl || out[0].Frames[1].Opcode != wire.OpAttachedEffect {
		t.Fatal("release sequence", out)
	}
	if binary.LittleEndian.Uint32(out[0].Frames[0].Payload[5:]) != 0 {
		t.Fatal("native release has no steering target")
	}
	effects := rt.effects.Snapshot(testDivision, c.Name)
	if len(effects) != 1 || effects[0].InstanceToken == castToken || effects[0].Phase != 1 {
		t.Fatal("recipient ownership", effects)
	}
	if *c.CurrentMP != mpBefore-int64(skill.Consumption.MP) {
		t.Fatal("release cost", *c.CurrentMP)
	}
	buffed, err := rt.PlayerBaseStats(testDivision, c)
	if err != nil || buffed.PhysicalDefense <= base.PhysicalDefense {
		t.Fatal("defense not installed", buffed, err)
	}
	if rt.hasOpenSkillCast(testDivision, c.Name) {
		t.Fatal("released root retained action lock")
	}
	end := deadline + 1 + int64(skill.EffectDurationMs)
	rt.effects.Expire(end)
	if effects = rt.effects.Snapshot(testDivision, c.Name); len(effects) != 1 || effects[0].StopRequested {
		t.Fatal("expired at equality")
	}
	rt.effects.Expire(end + 1)
	retired := rt.drainStoppedCharacterEffects()
	if len(retired) != 2 || retired[0].OnlyCharacterID != c.ID || len(retired[0].Frames) != 1 || retired[0].Frames[0].Opcode != wire.OpBaseStats || retired[1].OnlyCharacterID != 0 {
		t.Fatal("retirement must privately refresh stats before broadcasting ended tokens", retired)
	}
	after, err := rt.PlayerBaseStats(testDivision, c)
	if err != nil || after != base || len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
		t.Fatal("teardown left contribution", after, err)
	}
}

/*
================
TestTimedDefensePreparationInvalidation
================
*/
func TestTimedDefensePreparationInvalidation(t *testing.T) {
	for _, reason := range []string{"death", "cost", "unlearned", "cancel", "disconnect", "weapon"} {
		t.Run(reason, func(t *testing.T) {
			rt, clock, c, _ := newCombatTestRuntime(t, 100000)
			skill := shippedOffense(t, "SKILL_CH_COLD_GANGGI_A_01")
			source := rt.deps.SkillData().(staticSkillSource)
			source[skill.ID] = skill
			c.Skills = append(c.Skills, skill.ID)
			c.CurrentMP = testInt64(1000)
			start := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
			start = assertAndSeparateActionSession(t, start)
			assertOpcodes(t, start.Frames, wire.OpSkillCastResult)
			switch reason {
			case "death":
				c.CurrentHP = testInt64(0)
			case "cost":
				c.CurrentMP = testInt64(0)
			case "unlearned":
				c.Skills = c.Skills[:len(c.Skills)-1]
			case "cancel":
				rt.cancelPreparingProjectile(testDivision, c.Name)
			case "disconnect":
				rt.ForgetCharacter(testDivision, c.Name)
			case "weapon":
				skill.RequiredWeaponKinds = [2]uint8{255, 19}
				source[skill.ID] = skill
			}
			mp := *c.CurrentMP
			rt.advanceProjectileCasts(clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1)
			if len(rt.effects.Snapshot(testDivision, c.Name)) != 0 || *c.CurrentMP != mp {
				t.Fatal("invalidated preparation installed or charged")
			}
		})
	}
}
