package combat

import "opensro.online/server/internal/game/enterworld"

// Presence of the getv binding selects the branch, even if its dictionary
// entry is absent. Never fall through to a lower-priority learned value.
func parameterPercentile(point float64, values enterworld.SkillParameterValues, bindings enterworld.SkillParameterMask, magical bool) float64 {
	if bindings.Has(enterworld.ParameterDaggerHit) {
		point = float64(float32(point + float64(values[enterworld.ParameterDaggerHit])))
	}
	if bindings.Has(enterworld.ParameterDualHit) && (magical || !bindings.Has(enterworld.ParameterDaggerHit)) {
		point = float64(float32(point + float64(values[enterworld.ParameterDualHit])))
	}
	return point
}
func parameterAttackPoint(point float64, values enterworld.SkillParameterValues, bindings enterworld.SkillParameterMask, magical bool) float64 {
	apply := func(slot enterworld.SkillParameter) {
		if values[slot] != 0 {
			point = float64(float32(float64(float32(point)) * (1 + float64(values[slot])/100)))
		}
	}
	if !magical {
		// Physical lane weapon powers: mutually exclusive, unlike the magical lane.
		for _, slot := range [...]enterworld.SkillParameter{enterworld.ParameterDaggerPower, enterworld.ParameterCrossbowPower, enterworld.ParameterOneHandPower, enterworld.ParameterTwoHandPower, enterworld.ParameterDualPower} {
			if bindings.Has(slot) {
				apply(slot)
				break
			}
		}
	} else {
		// Magical lane: each present elemental/class modifier applies in order.
		for _, slot := range [...]enterworld.SkillParameter{enterworld.ParameterEarthPower, enterworld.ParameterColdPower, enterworld.ParameterFirePower, enterworld.ParameterLightningPower, enterworld.ParameterDotPower, enterworld.ParameterBloodPower, enterworld.ParameterMusicPower, enterworld.ParameterHolyPower} {
			if bindings.Has(slot) {
				apply(slot)
			}
		}
	}
	return point
}

// stealthStrikePoint is the physical lane's stealth step: a physical strike
// whose command was issued in stealth adds the caster's DGAA value,
// unsigned, to its attack point.
func stealthStrikePoint(point float64, attacker Stats, bindings enterworld.SkillParameterMask, magical bool) float64 {
	if magical || !attacker.StealthStrike || !bindings.Has(enterworld.ParameterStealthStrike) {
		return point
	}
	return float64(float32(point + float64(attacker.SkillParameters[enterworld.ParameterStealthStrike])))
}
