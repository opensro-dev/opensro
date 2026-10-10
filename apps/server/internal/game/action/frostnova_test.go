/*
===========================================================================

frostnova_test.go - Frost Nova freezes the ground through the status owner

The Chinese Cold line's Frost Nova (SKILL_CH_COLD_BINGPAN_) authors fz, fb,
efr and tant, no att and no action duration (#507). Tiers A to C are
targeted and select along the line to the primary (efr shape 4); tier D
names no target and selects around the caster (efr shape 1).

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
frostNovaSkill

The shipped row with a certain freeze roll, learned by a caster with the
MP to cast it and the weapon its columns name.
================
*/
func frostNovaSkill(t *testing.T, rt *Runtime, c *enterworld.Character, code string) enterworld.SkillRow {
	t.Helper()
	skill := shippedOffense(t, code)
	if !skill.StatusCast || skill.OffenseRefusal != "" || skill.ActionDurationMs != 0 {
		t.Fatalf("%s catalog shape: status=%v refusal=%q duration=%d", code, skill.StatusCast, skill.OffenseRefusal, skill.ActionDurationMs)
	}
	index, _ := abnormal.SourceIndex(0x667a) // fz
	skill.Abnormal.Params[index].Args[1] = 100
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = []uint32{skill.ID}
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(100000)
	if kind := skill.RequiredWeaponKinds[0]; kind != 255 {
		weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
		weapon.TypeIDs[3] = int64(kind)
		c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	}
	rt.CombatRoll = func() (uint32, error) { return 10, nil }
	return skill
}

/*
================
assertFrostNovaVictims

The three monsters beside the primary freeze without damage; the two 1000
away are untouched.
================
*/
func assertFrostNovaVictims(t *testing.T, rt *Runtime, targets []monster.Instance) {
	t.Helper()
	for i, target := range targets {
		after, _ := rt.Monsters.Get(testDivision, target.Gid)
		if after.CurrentHP != target.CurrentHP {
			t.Fatalf("monster %d took damage: HP %d -> %d", i, target.CurrentHP, after.CurrentHP)
		}
		frozen := after.Abnormal != nil && after.Abnormal.Slots[abnormal.Freeze].Active
		if near := i < 3; frozen != near {
			t.Fatalf("monster %d frozen=%v, want %v", i, frozen, near)
		}
	}
}

/*
================
TestFrostNovaFreezesAlongTheLineToItsTarget

Frost Nova-Wind 1: a targeted cast whose efr shape 4 selects the primary
and the monsters beside it, up to three.
================
*/
func TestFrostNovaFreezesAlongTheLineToItsTarget(t *testing.T) {
	rt, clock, c, targets := statusCastAreaFixture(t)
	skill := frostNovaSkill(t, rt, c, "SKILL_CH_COLD_BINGPAN_A_01")
	if !skill.TargetRequired || skill.OffensiveArea.Shape != 4 || skill.OffensiveArea.MaxTargets != 3 {
		t.Fatalf("tier A area %+v target %v", skill.OffensiveArea, skill.TargetRequired)
	}
	start := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: targets[0].Gid}.Encode())
	start = assertAndSeparateActionSession(t, start)
	if len(start.Frames) == 0 || len(rt.pendingProjectileCasts) != 1 {
		t.Fatalf("Frost Nova was not prepared: %+v", start)
	}
	tick := clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1
	clock.Advance(time.Duration(tick-clock.NowMs()+5) * time.Millisecond)
	if released := rt.advanceProjectileCasts(tick); len(released) == 0 {
		t.Fatal("prepared Frost Nova never released")
	}
	rt.advanceMonsterAbnormals(tick)
	assertFrostNovaVictims(t, rt, targets)
}

/*
================
TestFrostNovaIceFieldFreezesAroundTheCaster

Frost Nova-Ice Field 1 names no target: efr shape 1 selects around the
caster, and with no action duration the cast bracket closes at release.
================
*/
func TestFrostNovaIceFieldFreezesAroundTheCaster(t *testing.T) {
	rt, clock, c, targets := statusCastAreaFixture(t)
	skill := frostNovaSkill(t, rt, c, "SKILL_CH_COLD_BINGPAN_D_01")
	if skill.TargetRequired || skill.OffensiveArea.Shape != 1 {
		t.Fatalf("tier D area %+v target %v", skill.OffensiveArea, skill.TargetRequired)
	}
	out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
	if frame, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok || frame.Payload[0] != 1 || len(rt.pendingProjectileCasts) != 1 {
		t.Fatalf("Frost Nova-Ice Field was not prepared: %q %+v", out.DiagnosticRefusal, out.Frames)
	}
	release := clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1
	if released := rt.advanceProjectileCasts(release); len(released) == 0 {
		t.Fatal("prepared Frost Nova-Ice Field never released")
	}
	rt.advanceMonsterAbnormals(release)
	assertFrostNovaVictims(t, rt, targets)
	closed := false
	for _, batch := range rt.drainSkillFinalizes(release) {
		for _, f := range batch.Frames {
			closed = closed || f.Opcode == wire.OpSkillEffectControl && len(f.Payload) == 6 && f.Payload[0] == 2
		}
	}
	if !closed {
		t.Fatal("the cast bracket did not close at release")
	}
}
