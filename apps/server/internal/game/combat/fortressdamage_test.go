package combat

import (
	"errors"
	"testing"
)

/*
================
TestFortressDamageSpan

The structure draw spans the attacker's physical attack pair, one draw:
low bound included, high bound included, equal or inverted bounds deal
0 without drawing, and a draw error propagates.
================
*/
func TestFortressDamageSpan(t *testing.T) {
	attacker := Stats{PhysicalAttackMin: 100, PhysicalAttackMax: 200}
	for _, tc := range []struct {
		draw uint32
		want uint32
	}{{0, 100}, {137, 136}, {100, 200}, {101, 100}} {
		got, err := FortressDamage(attacker, func() (uint32, error) { return tc.draw, nil })
		if err != nil || got != tc.want {
			t.Fatalf("draw %d: got %d (%v), want %d", tc.draw, got, err, tc.want)
		}
	}
	draws := 0
	counting := func() (uint32, error) { draws++; return 7, nil }
	for _, flat := range []Stats{
		{PhysicalAttackMin: 50, PhysicalAttackMax: 50},
		{PhysicalAttackMin: 60, PhysicalAttackMax: 50},
	} {
		got, err := FortressDamage(flat, counting)
		if err != nil || got != 0 {
			t.Fatalf("flat bounds %+v: got %d (%v), want 0", flat, got, err)
		}
	}
	if draws != 0 {
		t.Fatalf("flat bounds consumed %d draws, want 0", draws)
	}
	if _, err := FortressDamage(attacker, func() (uint32, error) { return 0, errors.New("entropy failed") }); err == nil {
		t.Fatal("draw error swallowed")
	}
}
