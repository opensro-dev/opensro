/*
===========================================================================

structurehit.go - the target-side hit group: hits on fortress structures

The reference server's hit-outcome routine classifies a hit on a
fortress-class target into its own group, after the monster-shape group
and before the fortress-weapon group. The amount never touches the lane
formula: a uniform draw across the attacker's attack-power pair (the
physical pair, or the magical pair for the staff kinds), scaled by a
per-weapon-kind factor and truncated. Weapon kinds the factor switch
does not list (bare hand 1 among them) deal exactly 1 and draw nothing.

One draw per hit at most; the action runtime owns when this replaces
the lanes.

===========================================================================
*/

package combat

/*
================
structureHitKind

One weapon kind's row of the native factor switch: which attack-power
pair feeds the draw and the factor chain the draw is scaled by, in
order, with a float32 rounding after each multiply (the native float
stores). The constants are the native float factors of the switch.
================
*/
type structureHitKind struct {
	magical bool
	factors []float32
}

// structureHitKinds is indexed by weapon kind (the worn weapon's packed
// type word shifted right 11). Rows 0 and 1 stay empty: those kinds, and
// any kind above 16, deal exactly 1 without a draw. Kind 16 (the
// fortress weapons) also has no factor: the bare physical draw.
var structureHitKinds = []structureHitKind{
	{}, {}, // 0, 1
	{factors: []float32{0.30}},                          // 2
	{factors: []float32{0.90, 0.30}},                    // 3
	{factors: []float32{0.90, 0.58}},                    // 4
	{factors: []float32{0.90, 0.90, 0.58}},              // 5
	{factors: []float32{0.30, 0.42}},                    // 6
	{factors: []float32{0.66, 0.66}},                    // 7
	{factors: []float32{0.66}},                          // 8
	{factors: []float32{0.66, 0.38}},                    // 9
	{magical: true, factors: []float32{0.5, 0.5, 0.83}}, // 10
	{magical: true, factors: []float32{0.5, 0.5, 0.83}}, // 11
	{factors: []float32{0.47, 0.30}},                    // 12
	{factors: []float32{0.66, 0.25}},                    // 13
	{magical: true, factors: []float32{0.5, 0.5}},       // 14
	{magical: true, factors: []float32{0.5, 0.5, 0.83}}, // 15
	{}, // 16: the bare physical draw
}

/*
================
StructureHitDamage

The target-side group's amount for one hit: min + draw % (max-min+1)
across the attack-power pair the weapon kind selects, scaled by the
kind's factor chain and truncated. The pair is read as integers (the
native attribute read truncates its doubles). A pair with no span
yields 0 and consumes no draw, as natively; an unlisted kind yields
exactly 1 and also consumes no draw.
================
*/
func StructureHitDamage(weaponKind uint32, attacker Stats, roll Roll32767) (uint32, error) {
	if weaponKind < 2 || weaponKind > 16 {
		return 1, nil
	}
	kind := structureHitKinds[weaponKind]
	minimum, maximum := attacker.PhysicalAttackMin, attacker.PhysicalAttackMax
	if kind.magical {
		minimum, maximum = attacker.MagicalAttackMin, attacker.MagicalAttackMax
	}
	lo, hi := int(minimum), int(maximum)
	span := hi - lo
	if span <= 0 {
		return 0, nil
	}
	draw, err := roll()
	if err != nil {
		return 0, err
	}
	value := float64(lo + int(draw%uint32(span+1)))
	for _, factor := range kind.factors {
		value = float64(float32(value * float64(factor)))
	}
	return uint32(float32(value)), nil
}
