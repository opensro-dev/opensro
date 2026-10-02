/*
===========================================================================

skilltarget_test.go - native player-target flag branches

The native RefSkill target-group bytes start at +98, after the type bytes.
EnemyP is +9C; +9F selects corpses and must never be confused with it.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"testing"
)

/*
================
TestSkillTargetPermissionNativeArms
================
*/
func TestSkillTargetPermissionNativeArms(t *testing.T) {
	cases := []struct {
		name                     string
		flags                    enterworld.SkillTargets
		self, alive, party, want bool
	}{
		{"enemy player", enterworld.SkillTargets{Animal: true, EnemyP: true}, false, true, false, true},
		{"enemy monster only", enterworld.SkillTargets{Animal: true, EnemyM: true}, false, true, false, false},
		{"corpse refuses living", enterworld.SkillTargets{Animal: true, DeadBody: true}, false, true, false, false},
		{"corpse admits dead", enterworld.SkillTargets{Animal: true, DeadBody: true}, false, false, false, true},
		{"self missing", enterworld.SkillTargets{Animal: true, EnemyP: true}, true, true, false, false},
		{"self allowed", enterworld.SkillTargets{Animal: true, Self: true}, true, true, false, true},
		{"party missing", enterworld.SkillTargets{Animal: true, Party: true}, false, true, false, false},
		{"party shared", enterworld.SkillTargets{Animal: true, Party: true}, false, true, true, true},
		{"ally bypasses party restriction", enterworld.SkillTargets{Animal: true, Ally: true, Party: true}, false, true, false, true},
		{"dont care", enterworld.SkillTargets{DontCare: true, DeadBody: true}, true, true, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := skillTargetPermission(c.flags, c.self, c.alive, c.party); got != c.want {
				t.Fatalf("admission=%v want=%v", got, c.want)
			}
		})
	}
}
