/*
===========================================================================

skill_cooldown_test.go - native action and reuse timing boundaries

Exercise the eligibility branch and the unscaled reuse remainder separately.

===========================================================================
*/

package enterworld

import "testing"

/*
================
TestCooldownDurationKeepsUnscaledRemainder
================
*/
func TestCooldownDurationKeepsUnscaledRemainder(t *testing.T) {
	row := SkillRow{ActionKind: 2, ActionCastingTimeMs: 300, ActionDurationMs: 700, CoolTimeMs: 3000}
	for _, sample := range []struct {
		percent       float32
		reuse, action uint32
	}{{100, 3000, 1000}, {200, 4000, 2000}, {125, 3250, 1250}, {250, 4500, 2500}} {
		if got := row.CooldownDurationMs(sample.percent); got != sample.reuse {
			t.Errorf("reuse at %v: %d, want %d", sample.percent, got, sample.reuse)
		}
		if got := row.ActionRecoveryDurationMs(sample.percent); got != sample.action {
			t.Errorf("action at %v: %d, want %d", sample.percent, got, sample.action)
		}
	}
	row.CoolTimeMs = 500
	if row.CooldownDurationMs(125) != 1250 {
		t.Fatal("negative reuse remainder shortened the scaled action")
	}
	row.ActionKind = 1
	if row.CooldownDurationMs(200) != 500 || row.ActionRecoveryDurationMs(200) != 0 {
		t.Fatal("non-type-two action was scaled")
	}
	row.ActionKind, row.ActionDurationMs = 2, 0
	if row.CooldownDurationMs(200) != 500 || row.ActionRecoveryDurationMs(200) != 0 {
		t.Fatal("zero-duration action was scaled")
	}
	row.ActionDurationMs, row.ActionCastingTimeMs = 701, 300
	if row.CooldownDurationMs(125) != 1251 || row.ActionRecoveryDurationMs(125) != 1251 {
		t.Fatal("fractional scaled duration was not truncated")
	}
}
