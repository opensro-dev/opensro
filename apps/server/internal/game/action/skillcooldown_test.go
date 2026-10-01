/*
===========================================================================

skillcooldown_test.go - live status timing across skill families

The admitted deadline is a snapshot. Cures affect later actions, and changing
skill group cannot bypass the common recovery timer.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"testing"
)

/*
================
TestStatusCooldownRegistrationAndRecoveryGate
================
*/
func TestStatusCooldownRegistrationAndRecoveryGate(t *testing.T) {
	for _, sample := range []struct {
		status        abnormal.Status
		reuse, action int64
	}{{abnormal.Frostbite, 4000, 2000}, {abnormal.Slow, 3250, 1250}} {
		rt, clock, c, source := newCombatTestRuntime(t, 100)
		now := clock.NowMs()
		rt.applyPlayerAbnormalInDoor(testDivision, c, false, []abnormal.Record{{
			Status: sample.status, Level: 1, Grade: 1, DurationMs: 1000, SourceGID: source.Gid,
		}}, now)
		row := enterworld.SkillRow{ID: 1, Group: 1, CoolTimeGroup: 1,
			ActionKind: 2, ActionCastingTimeMs: 300, ActionDurationMs: 700, CoolTimeMs: 3000}
		rt.registerPlayerSkillCooldown(testDivision, c, row, now)
		if c.OffensiveSkillCooldowns[1] != now+sample.reuse || c.SharedSkillCooldowns[1] != now+sample.reuse ||
			c.SkillActionRecoveryUntilMs != now+sample.action {
			t.Fatalf("status %d: reuse %v shared %v recovery %d", sample.status,
				c.OffensiveSkillCooldowns, c.SharedSkillCooldowns, c.SkillActionRecoveryUntilMs)
		}
		other := row
		other.ID, other.Group, other.CoolTimeGroup = 2, 2, 2
		mask := admitCooldown | admitActionRecovery
		if got := rt.skillAdmission(testDivision, c, other, now+sample.action-1, nil, nil, mask); got != 0x3005 {
			t.Fatalf("different group bypassed action recovery: %x", got)
		}
		if got := rt.skillAdmission(testDivision, c, other, now+sample.action, nil, nil, mask); got != 0 {
			t.Fatalf("recovery equality refused: %x", got)
		}
		if got := rt.skillAdmission(testDivision, c, other, now, nil, nil, admitCooldown); got != 0 {
			t.Fatalf("command phase ran execution recovery gate: %x", got)
		}
		other.ChainNext = 3
		if got := rt.skillAdmission(testDivision, c, other, now, nil, nil, mask); got != 0 {
			t.Fatalf("chain link refused by common recovery: %x", got)
		}
		rt.advancePlayerAbnormals(now + 1001)
		if c.OffensiveSkillCooldowns[1] != now+sample.reuse || rt.playerSkillCooldown(testDivision, c, row) != 3000 {
			t.Fatal("expiry changed the admitted deadline or retained the penalty for a new action")
		}
	}
}

/*
================
TestBasicAttackRegistersStatusRecovery

Exercise the public engage lane with and without area targeting. Resource
charging must not decide whether an admitted action owns a recovery timer.
================
*/
func TestBasicAttackRegistersStatusRecovery(t *testing.T) {
	for _, area := range []bool{false, true} {
		rt, clock, c, target := newCombatTestRuntime(t, 10000)
		skills := rt.deps.SkillData().(staticSkillSource)
		row := skills[2]
		row.ActionKind, row.Group = 2, 2
		row.Consumption.Pinned = true
		if area {
			row.OffensiveArea = enterworld.SkillOffensiveArea{Radius: 10, Shape: 2, MaxTargets: 1}
		}
		skills[2] = row
		now := clock.NowMs()
		rt.applyPlayerAbnormalInDoor(testDivision, c, false, []abnormal.Record{{
			Status: abnormal.Frostbite, Level: 1, Grade: 1, DurationMs: 10000, SourceGID: target.Gid,
		}}, now)
		result := rt.HandleTargetInteract(testDivision, c, wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
		if result.DiagnosticRefusal != "" || c.SkillActionRecoveryUntilMs != now+2400 || c.OffensiveSkillCooldowns[2] != now+2400 {
			t.Fatalf("area %v: recovery %d cooldown %v result %+v", area,
				c.SkillActionRecoveryUntilMs, c.OffensiveSkillCooldowns, result)
		}
	}
}

/*
================
TestCooldownLookupSelectsNativeMapAndZeroBypass
================
*/
func TestCooldownLookupSelectsNativeMapAndZeroBypass(t *testing.T) {
	c := &enterworld.Character{OffensiveSkillCooldowns: map[uint32]int64{1: 1000},
		SharedSkillCooldowns: map[uint8]int64{2: 1000}}
	row := enterworld.SkillRow{Group: 1, CoolTimeGroup: 2}
	if skillCoolingDown(c, row, 1) {
		t.Fatal("authored zero duration read a retained cooldown")
	}
	row.CoolTimeMs, row.CoolTimeGroup = 1000, 3
	if skillCoolingDown(c, row, 1) {
		t.Fatal("shared-group skill also read its individual map")
	}
	row.CoolTimeGroup = 0
	if !skillCoolingDown(c, row, 1) || skillCoolingDown(c, row, 1000) {
		t.Fatal("individual cooldown did not honor its exact deadline")
	}
}
