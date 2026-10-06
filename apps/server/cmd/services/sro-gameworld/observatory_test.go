/*
===========================================================================

observatory_test.go - diagnostic character gauge semantics

Fresh characters must not appear dead merely because their full gauges are
implicit in storage. Explicit death and boosted stored gauges stay intact.

===========================================================================
*/

package main

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestObservatoryPlayerVitals
================
*/
func TestObservatoryPlayerVitals(t *testing.T) {
	zero, negative, damaged, boosted := int64(0), int64(-1), int64(73), int64(450)
	strength, intellect := int64(30), int64(25)
	tests := []struct {
		name      string
		character domain.Character
		hp, mp    int64
	}{
		{name: "fresh", hp: 200, mp: 200},
		{name: "derived stats", character: domain.Character{Strength: &strength, Intellect: &intellect}, hp: 300, mp: 250},
		{name: "dead and empty", character: domain.Character{CurrentHP: &zero, CurrentMP: &zero}},
		{name: "negative", character: domain.Character{CurrentHP: &negative, CurrentMP: &negative}},
		{name: "damaged", character: domain.Character{CurrentHP: &damaged, CurrentMP: &damaged}, hp: 73, mp: 73},
		{name: "boosted stored", character: domain.Character{CurrentHP: &boosted, CurrentMP: &boosted}, hp: 450, mp: 450},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			row := captureObservatoryPlayer(&test.character, simulation.SessionSnapshot{}, 0)
			if row.HP != test.hp || row.MP != test.mp {
				t.Fatalf("gauges = (%d, %d), want (%d, %d)", row.HP, row.MP, test.hp, test.mp)
			}
		})
	}
}
