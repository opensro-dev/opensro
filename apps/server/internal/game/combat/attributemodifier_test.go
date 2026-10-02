/*
===========================================================================

attributemodifier_test.go - combined native HP bonuses and penalties

An isolated percentage cannot distinguish keeper channels. Exercise the
interaction with a second owner so Life Control's native channel matters.

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
TestLifeControlPenaltyAddsToOtherHPPercentages

59613F..59616A: 1000 base HP, +20 percent and -50 percent give 700,
not the 600 produced by multiplying the penalty after the bonus.
================
*/
func TestLifeControlPenaltyAddsToOtherHPPercentages(t *testing.T) {
	hp, err := paramkeeper.New(paramkeeper.Definition{Base: 1000, Maximum: 100000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hp.Apply(paramkeeper.PercentSum, 1, 20); err != nil {
		t.Fatal(err)
	}
	for _, write := range AttributeEffectWrites(enterworld.SkillAttributeBoost{MaxHPPenalty: true, HPPenaltyPercent: 50}) {
		if _, err := hp.Apply(write.Channel, 2, write.Value); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := hp.Value(); err != nil || got != 700 {
		t.Fatalf("combined maximum HP = %v, %v; want 700", got, err)
	}
	hp.Remove(2)
	if got, err := hp.Value(); err != nil || got != 1200 {
		t.Fatalf("retired maximum HP = %v, %v; want 1200", got, err)
	}
}
