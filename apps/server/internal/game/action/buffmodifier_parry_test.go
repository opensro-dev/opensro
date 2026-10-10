/*
===========================================================================

buffmodifier_parry_test.go - Concentration's er writes the parry keeper

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/paramkeeper"
)

/*
================
TestConcentrationWritesParryFlatAndRate

594AC0 0x595883..0x5958DE: er's flat on channel 0 and rate on channel 1 of
parameter 9, the shape hr has on parameter 11.
================
*/
func TestConcentrationWritesParryFlatAndRate(t *testing.T) {
	writes := buffModifierWrites(enterworld.SkillBuffModifiers{Er: true, ErFlat: 30, ErRate: 15}, false)
	var flat, rate bool
	for _, w := range writes {
		if w.Parameter != itemParamEvasion {
			t.Fatalf("er wrote parameter %d", w.Parameter)
		}
		switch w.Channel {
		case paramkeeper.Flat:
			flat = w.Value == 30
		case paramkeeper.PercentSum:
			rate = w.Value == 15
		}
	}
	if len(writes) != 2 || !flat || !rate {
		t.Fatalf("er writes %+v", writes)
	}
	if len(buffModifierWrites(enterworld.SkillBuffModifiers{}, false)) != 0 {
		t.Fatal("no modifiers wrote parameters")
	}
}
