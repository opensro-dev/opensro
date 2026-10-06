/*
===========================================================================

fortress_death_test.go - special-world death EXP cap from 4E6D60

===========================================================================
*/
package progression

import (
	"opensro.online/server/internal/game/pk"
	"testing"
)

/*
================
siegeDeathLevels
================
*/
type siegeDeathLevels struct {
	staticLevels
	required, basis int64
}

/*
================
ExpRequired
================
*/
func (s siegeDeathLevels) ExpRequired(int64) (int64, bool) { return s.required, true }

/*
================
MonsterExpBasis
================
*/
func (s siegeDeathLevels) MonsterExpBasis(int64) (int64, bool) { return s.basis, true }

/*
================
TestFortressDeathPenaltyUsesNativeOneFifthCap
================
*/
func TestFortressDeathPenaltyUsesNativeOneFifthCap(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		player                      bool
		required, basis, kept, want int64
	}{
		{"player cap", true, 1000000, 7, 0, 28},
		{"monster cap", false, 1000000, 7, 0, 140},
		{"percent before cap", false, 1000, 7, 0, 19},
		{"zero cap clamps", true, 1000, 0, 0, 1},
		{"premium after cap", true, 1000000, 7, 50, 14},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule, _ := pk.DeathLoss(pk.DeathPlayer, 20, 0, tc.player, false)
			got, ok := deathPenaltyLoss(siegeDeathLevels{required: tc.required, basis: tc.basis}, 20, pk.DeathPenalty{Rule: rule, SpecialWorld: true, ReductionPercent: float32(tc.kept)})
			if !ok || got != tc.want {
				t.Fatalf("loss=%d %v want %d", got, ok, tc.want)
			}
		})
	}
}
