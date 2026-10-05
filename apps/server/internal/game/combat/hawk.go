/*
===========================================================================

hawk.go - the attacking hawk's strike damage (Black and Light Hawk Summon)

Skill_ProcessPeriodicDamage (582750) adds two flat-base formulas, the
physical 40F1B0 and the magical 40F3D0, both read on the struck target:
defense, parry, the basic-hit absorption, the incoming factor. Neither rolls
an attack interval or a hit; the hawk always lands. Every float the
originals store with fstp dword is rounded to float32 at the same point.

===========================================================================
*/

package combat

import "math"

// hawkDamageCeiling is the float 40F38D stores over a larger result.
const hawkDamageCeiling = 16777215

/*
================
hawkLane

One of the hawk's two formulas: the summ word that is its flat base, and
the target's reads it takes. Physical (40F1B0) reads parameters 5 and 7,
absorption 410BB0(5) and 0xB4; magical (40F3D0) reads 6 and 8, 410BB0(9)
and 0xB5.
================
*/
type hawkLane struct {
	base            uint32
	defense, parry  float64
	taken, incoming float32
	integerFloor    bool
}

/*
================
HawkDamage

The u16 sum 582750 sends: physical plus magical, never zero. rank is the
hawk row's mastery rank (59E770: zero without MAAT, as the summ rows are).
A lane whose base word is zero returns zero without rolling (40F1B0 /
40F3D0 test it first).
================
*/
func HawkDamage(defender Stats, physical, magical uint32, rank uint8, roll Roll32767) (uint16, error) {
	lanes := [2]hawkLane{
		{base: physical, defense: defender.PhysicalDefense, parry: defender.ParryRate,
			taken: defender.PhysicalBasicTaken, incoming: defender.PhysicalIncoming},
		{base: magical, defense: defender.MagicalDefense, parry: defender.MagicalParry,
			taken: defender.MagicalBasicTaken, incoming: defender.MagicalIncoming, integerFloor: true},
	}
	var sum uint16
	for _, lane := range lanes {
		value, err := hawkLaneDamage(lane, rank, roll)
		if err != nil {
			return 0, err
		}
		sum += value
	}
	if sum == 0 {
		sum = 1
	}
	return sum, nil
}

/*
================
hawkLaneDamage

40F1B0 / 40F3D0 after their live checks. The minimum-damage fallback is the
only difference: 40F1B0 draws a float between one and 10% of the base,
40F3D0 between one and that ceiling truncated to an integer.
================
*/
func hawkLaneDamage(lane hawkLane, rank uint8, roll Roll32767) (uint16, error) {
	if lane.base == 0 {
		return 0, nil
	}
	base := float32((float64(rank)/100 + 1) * float64(lane.base))
	defense := float32(lane.defense)
	parry := float32(float64(float32(lane.parry)) / 100)
	value := float32(float64(base)/(float64(parry)+1) - float64(defense))
	if value < 0 {
		value = 0
	}
	if lane.taken != 0 {
		value = float32(float64(lane.taken) * float64(value))
	}
	if 0.05*float64(base) > float64(value) {
		draw, err := normalizedRoll(roll)
		if err != nil {
			return 0, err
		}
		r := float32(draw)
		if lane.integerFloor {
			ceiling := int32(float64(base) * 10 / 100)
			if ceiling <= 0 {
				ceiling = 1
			}
			value = float32(float64(r)*float64(ceiling-1) + 1)
		} else {
			value = float32(float64(base) * 10 / 100)
			if !(value > 0) {
				value = 1
			}
			value = float32(1 + (float64(value)-1)*float64(r))
		}
	}
	if lane.incoming != 0 {
		value = float32(float64(lane.incoming) * float64(value))
	}
	switch {
	case value < 0:
		value = 0
	case float64(value) >= hawkDamageCeiling:
		value = hawkDamageCeiling
	}
	// The caller keeps the low word of the truncated result (582865 movzx).
	return uint16(int64(math.Trunc(float64(value)))), nil
}
