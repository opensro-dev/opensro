/*
===========================================================================

shieldattack_test.go - spda's truncated percents of the shield's defense

===========================================================================
*/

package combat

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/paramkeeper"
)

/*
================
TestShieldAttackWritesTruncateEachLane

17 % and 27 % of 123.4 are 20.978 and 33.318: _ftol keeps 20 and 33. A lane
whose product truncates to zero writes nothing (the jle at 59516C/595194).
================
*/
func TestShieldAttackWritesTruncateEachLane(t *testing.T) {
	got := ShieldAttackWrites(enterworld.SkillShieldAttack{Present: true, DefensePercent: 17, AttackPercent: 27}, 123.4)
	want := []paramkeeper.Write{
		{Parameter: attributePhysicalDefense, Channel: paramkeeper.Flat, Value: -20},
		{Parameter: attributePhysicalMin, Channel: paramkeeper.Flat, Value: 33},
		{Parameter: attributePhysicalMax, Channel: paramkeeper.Flat, Value: 33},
	}
	if len(got) != len(want) {
		t.Fatalf("writes %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("write %d %+v, want %+v", i, got[i], want[i])
		}
	}
	if weak := ShieldAttackWrites(enterworld.SkillShieldAttack{Present: true, DefensePercent: 17, AttackPercent: 27}, 3.5); len(weak) != 0 {
		t.Fatalf("sub-point lanes wrote %+v", weak)
	}
}
