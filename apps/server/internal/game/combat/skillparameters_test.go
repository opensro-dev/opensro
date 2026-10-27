package combat

import (
	"opensro.online/server/internal/game/enterworld"
	"testing"
)

func TestNativeParameterLanesAndPrecedence(t *testing.T) {
	// Independent simple oracle: a 100 point with a 50% scalar becomes 150,
	// only in the native lane. The two hit keys change percentile, not damage.
	for slot := enterworld.SkillParameter(0); slot < enterworld.SkillParameterCount; slot++ {
		values := enterworld.SkillParameterValues{}
		values[slot] = 50
		mask := enterworld.SkillParameterMask(1 << slot)
		for _, magic := range []bool{false, true} {
			want := 100.0
			if slot <= enterworld.ParameterDaggerPower && !magic || slot >= enterworld.ParameterEarthPower && slot <= enterworld.ParameterHolyPower && magic {
				want = 150
			}
			if got := parameterAttackPoint(100, values, mask, magic); got != want {
				t.Fatalf("slot %d magical %v got %v want %v", slot, magic, got, want)
			}
		}
	}
	values := enterworld.SkillParameterValues{enterworld.ParameterDaggerPower: 10, enterworld.ParameterTwoHandPower: 90, enterworld.ParameterFirePower: 20, enterworld.ParameterEarthPower: 50}
	mask := enterworld.SkillParameterMask(1<<enterworld.ParameterDaggerPower | 1<<enterworld.ParameterTwoHandPower | 1<<enterworld.ParameterFirePower | 1<<enterworld.ParameterEarthPower)
	if got := parameterAttackPoint(100, values, mask, false); got != 110 {
		t.Fatal("physical factors stacked", got)
	}
	if got := parameterAttackPoint(100, values, mask, true); got != 180 {
		t.Fatal("magical factors did not multiply", got)
	}
	values[enterworld.ParameterDaggerPower] = 0
	if got := parameterAttackPoint(100, values, mask, false); got != 100 {
		t.Fatal("absent high-priority value fell through", got)
	}
	values = enterworld.SkillParameterValues{enterworld.ParameterDaggerHit: 10, enterworld.ParameterDualHit: 20}
	mask = 1<<enterworld.ParameterDaggerHit | 1<<enterworld.ParameterDualHit
	if got := parameterPercentile(25, values, mask, false); got != 35 {
		t.Fatal(got)
	}
	if got := parameterPercentile(25, values, mask, true); got != 55 {
		t.Fatal(got)
	}
}

func TestParameterValueSnapshotsAreDetached(t *testing.T) {
	a := Stats{SkillParameters: enterworld.SkillParameterValues{enterworld.ParameterFirePower: 50}}
	b := a
	b.SkillParameters[enterworld.ParameterFirePower] = 100
	if a.SkillParameters[enterworld.ParameterFirePower] != 50 {
		t.Fatal("formula aliases mutable parameters")
	}
}

// The physical lane's stealth step: DGAA is a flat physical addition, only
// for a stealth command and only on a row that asks for it.
func TestStealthStrikePoint(t *testing.T) {
	bound := enterworld.SkillParameterMask(1) << enterworld.ParameterStealthStrike
	var attacker Stats
	attacker.SkillParameters[enterworld.ParameterStealthStrike] = 62
	attacker.StealthStrike = true

	if got := stealthStrikePoint(100, attacker, bound, false); got != 162 {
		t.Fatalf("stealth strike %g, want 162", got)
	}
	if got := stealthStrikePoint(100, attacker, bound, true); got != 100 {
		t.Fatalf("magical lane took DGAA: %g", got)
	}
	if got := stealthStrikePoint(100, attacker, 0, false); got != 100 {
		t.Fatalf("unbound row took DGAA: %g", got)
	}
	attacker.StealthStrike = false
	if got := stealthStrikePoint(100, attacker, bound, false); got != 100 {
		t.Fatalf("open strike took DGAA: %g", got)
	}
}
