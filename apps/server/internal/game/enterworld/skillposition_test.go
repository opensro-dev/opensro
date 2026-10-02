/*
===========================================================================

skillposition_test.go - ground-travel program admission

===========================================================================
*/

package enterworld

import "testing"

/*
================
TestPositionProgramFailsClosed

tele and tel2 with caster getv modifiers admit; any other instruction,
range or envelope refuses.
================
*/
func TestPositionProgramFailsClosed(t *testing.T) {
	for _, mode := range []string{"tele", "extra", "tel2", "getv", "unknown-getv", "negative-range", "zero-range", "linked", "casting", "hp-cost"} {
		f := make([]string, 118)
		for i := range f {
			f[i] = "0"
		}
		f[0] = "1"
		f[69] = "1952803941"
		f[70] = "500"
		f[71] = "215"
		r := SkillRow{ID: 1, TargetRequired: true, TimingPinned: true, Consumption: SkillConsumption{Pinned: true}}
		switch mode {
		case "extra":
			f[72] = "25202"
			f[73] = "10"
		case "tel2":
			f[69] = "1952803890"
		case "getv": // WIMD only adjusts the prepared cost
			f[72], f[73] = "1734702198", "1464421700"
		case "unknown-getv":
			f[72], f[73] = "1734702198", "1"
		case "negative-range":
			f[71] = "-1"
		case "zero-range":
			f[71] = "0"
		case "linked":
			r.ChainNext = 2
		case "casting":
			r.ActionCastingTimeMs = 1
		case "hp-cost":
			r.Consumption.HP = 1
		}
		r.PositionEffect = decodeSkillPosition(f, r)
		if r.PositionEffect.Pinned != (mode == "tele" || mode == "tel2" || mode == "getv") {
			t.Fatalf("%s %+v", mode, r.PositionEffect)
		}
		if compactSkill(r).value().PositionEffect != r.PositionEffect {
			t.Fatal("storage dropped movement descriptor")
		}
	}
}
