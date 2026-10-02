/*
===========================================================================

lifecontrol_test.go - Wizard Life Control through the timed self-effect owner

The buff trades half of the maximum HP for magical attack and damage rate,
and retires every contribution together.

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestLifeControlTradesMaximumHPForMagicalDamage

apau raises both magical bounds, dru the magical damage rates and pmhp halves
the maximum HP. Expiry restores every parameter.
================
*/
func TestLifeControlTradesMaximumHPForMagicalDamage(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 1000000)
	skill := shippedOffense(t, "SKILL_EU_WIZARD_MENTALA_DAMAGEUP_A_01")
	if !skill.TimedEffect.Pinned {
		t.Fatalf("Life Control not admitted: %q", skill.OffenseRefusal)
	}
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.RaceIndex = testInt64(enterworld.RaceEurope)
	c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	c.Skills = []uint32{skill.ID}
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(10000)
	// reqi 6 11: the Wizard's staff.
	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 11
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	base, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}

	out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
	out = assertAndSeparateActionSession(t, out)
	assertOpcodes(t, out.Frames, wire.OpSkillCastResult)
	released := clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1
	rt.advanceProjectileCasts(released)
	if len(rt.effects.Snapshot(testDivision, c.Name)) != 1 {
		t.Fatal("Life Control was not installed")
	}
	// The release frees the caster; a detached close retires the cast aura.
	if rt.hasOpenSkillCast(testDivision, c.Name) {
		t.Fatal("released buff retained the caster's action")
	}
	closed := false
	for _, batch := range rt.drainSkillFinalizes(released + int64(skill.ActionDurationMs)) {
		for _, f := range batch.Frames {
			closed = closed || f.Opcode == wire.OpSkillEffectControl && len(f.Payload) == 6 && f.Payload[0] == 2
		}
	}
	if !closed {
		t.Fatal("Life Control never closed its cast bracket")
	}
	buffed, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	baseHP, _ := base.Param(3)
	buffedHP, _ := buffed.Param(3)
	if buffedHP != baseHP*0.5 {
		t.Errorf("maximum HP %v, want half of %v", buffedHP, baseHP)
	}
	for _, parameter := range []uint16{0x0f, 0x10} {
		before, _ := base.Param(parameter)
		after, _ := buffed.Param(parameter)
		if after != before+59 {
			t.Errorf("magical attack %x = %v, want %v", parameter, after, before+59)
		}
	}
	if buffed.MagicalBasicRate != base.MagicalBasicRate+25 || buffed.MagicalSkillRate != base.MagicalSkillRate+25 ||
		buffed.PhysicalSkillRate != base.PhysicalSkillRate {
		t.Errorf("damage rates %v/%v/%v, base %v/%v/%v", buffed.MagicalBasicRate, buffed.MagicalSkillRate,
			buffed.PhysicalSkillRate, base.MagicalBasicRate, base.MagicalSkillRate, base.PhysicalSkillRate)
	}

	rt.effects.Expire(released + int64(skill.EffectDurationMs) + 1)
	rt.drainStoppedCharacterEffects()
	after, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	for _, parameter := range []uint16{3, 0x0f, 0x10} {
		want, _ := base.Param(parameter)
		got, _ := after.Param(parameter)
		if got != want {
			t.Errorf("retired parameter %x = %v, want %v", parameter, got, want)
		}
	}
	if after.MagicalSkillRate != base.MagicalSkillRate {
		t.Errorf("retired magical rate %v, want %v", after.MagicalSkillRate, base.MagicalSkillRate)
	}
}

/*
================
TestLifeTurnoverReplacesLifeControl

The deliberate tier deviation through the production owner: Life Turnover
cast over an active Life Control retires it and installs itself, while a
Life Control cast over Life Turnover is refused.
================
*/
func TestLifeTurnoverReplacesLifeControl(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 1000000)
	control := shippedOffense(t, "SKILL_EU_WIZARD_MENTALA_DAMAGEUP_A_01")
	turnover := shippedOffense(t, "SKILL_EU_WIZARD_MENTALA_DAMAGEUP_B_01")
	for _, skill := range []enterworld.SkillRow{control, turnover} {
		rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	}
	c.RaceIndex = testInt64(enterworld.RaceEurope)
	c.Skills = []uint32{control.ID, turnover.ID}
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(100000)
	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 11
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	cast := func(skill enterworld.SkillRow) bool {
		now := clock.NowMs()
		out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
		frame, ok := findFrame(out.Frames, wire.OpSkillCastResult)
		if !ok || frame.Payload[0] != 1 {
			return false
		}
		rt.advanceProjectileCasts(now + int64(skill.ActionCastingTimeMs) + 1)
		rt.drainStoppedCharacterEffects()
		// Both tiers share one reuse group: wait it out before the next cast.
		wait := max(skill.ActionCastingTimeMs+skill.ActionDurationMs, skill.CoolTimeMs) + 1
		clock.Advance(time.Duration(wait) * time.Millisecond)
		rt.drainSkillFinalizes(clock.NowMs())
		return true
	}
	active := func() []uint32 {
		var ids []uint32
		for _, e := range rt.effects.Snapshot(testDivision, c.Name) {
			if !e.StopRequested {
				ids = append(ids, e.SkillID)
			}
		}
		return ids
	}
	if !cast(control) || len(active()) != 1 || active()[0] != control.ID {
		t.Fatalf("Life Control not installed: %v", active())
	}
	if !cast(turnover) || len(active()) != 1 || active()[0] != turnover.ID {
		t.Fatalf("Life Turnover did not replace Life Control: %v", active())
	}
	cast(control)
	if ids := active(); len(ids) != 1 || ids[0] != turnover.ID {
		t.Fatalf("Life Control replaced the stronger Life Turnover: %v", ids)
	}
}
