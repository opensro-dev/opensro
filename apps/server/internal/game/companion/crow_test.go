/*
===========================================================================
crow_test.go - port-only crow rank endpoints and cumulative book progression.
===========================================================================
*/
package companion

import (
	"math"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestCrowBookProgression
================
*/
func TestCrowBookProgression(t *testing.T) {
	p := &domain.CharacterCOS{Codename: "COS_P_RAVEN_090", Training: domain.PetTraining{TreeVersion: 2}}
	for _, test := range []struct {
		first                                              uint8
		valueFirst, valueLast, cooldownFirst, cooldownLast float64
	}{{46, 10, 30, 20, 15}, {49, 30, 30, 1200, 900}, {52, 10, 30, 0, 0}, {55, 2, 5, 0, 0}, {58, 20, 40, 300, 120}} {
		first, _ := PetSkill(p, test.first)
		last, _ := PetSkill(p, test.first+2)
		if math.Abs(first.ValueAt(1)-test.valueFirst) > 1e-8 || math.Abs(last.ValueAt(10)-test.valueLast) > 1e-8 ||
			math.Abs(first.CooldownAt(1)-test.cooldownFirst) > 1e-8 || math.Abs(last.CooldownAt(10)-test.cooldownLast) > 1e-8 {
			t.Fatal("crow progression endpoints", first.Name)
		}
		for id := test.first + 1; id <= test.first+2; id++ {
			previous, _ := PetSkill(p, id-1)
			next, _ := PetSkill(p, id)
			if next.ValueAt(1) < previous.ValueAt(10) || next.CooldownAt(1) > previous.CooldownAt(10) {
				t.Fatal("new book weakened the previous book", next.Name)
			}
		}
		if last.ValueAt(30) > test.valueLast || last.CooldownAt(30) < test.cooldownLast {
			t.Fatal("gear skill ranks escaped explicit crow caps", last.Name)
		}
	}
	crow, penguin := Profile(p.Codename), Profile("COS_P_PENGUIN_090")
	if !crow.MagicPrimary || math.Abs(crow.MagicalPercent-penguin.MagicalPercent*.7) > 1e-8 {
		t.Fatal("crow magic role must be seventy percent of penguin", crow)
	}
}
