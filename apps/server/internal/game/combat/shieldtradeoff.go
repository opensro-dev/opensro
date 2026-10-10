/*
===========================================================================

shieldtradeoff.go - Flying Heaven Art's shield-only defense and attack tradeoff

The effect registry owns the percentage and flat attack contributions. The
combat snapshot applies the penalty to the current shield, including its
STR reinforcement, so a shield replacement or STR change cannot leave a
stale installation-time defense cut. Armor and unrelated defense buffs keep
their own contributions.

===========================================================================
*/
package combat

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/paramkeeper"
)

const (
	// Projection-only node: the original server keeper mapping is unknown.
	shieldDefensePenaltyParameter uint16 = 0x1fe
	// Detached derived source: outside native small owners and equipment/passives.
	shieldDefensePenaltySource uint32 = 512
)

const shieldPenaltyPercent = 100

/*
================
ShieldTradeoffWrites

spda's first word is held in an isolated projection node; its second
adds to physical min/max attack. The client proves the units
(7FC4CA..7FC5B6), not the server keeper mapping. No owner-supplied
v1.188 server image is available for that mapping. Never alias a native
keeper such as 0x38 (block ignore) to carry the shield penalty.
================
*/
func ShieldTradeoffWrites(rule enterworld.SkillShieldTradeoff) []paramkeeper.Write {
	if !rule.Present {
		return nil
	}
	return []paramkeeper.Write{
		{Parameter: shieldDefensePenaltyParameter, Channel: paramkeeper.Flat, Value: float32(rule.DefensePercent)},
		{Parameter: attributePhysicalMin, Channel: paramkeeper.Flat, Value: float32(rule.PhysicalAttack)},
		{Parameter: attributePhysicalMax, Channel: paramkeeper.Flat, Value: float32(rule.PhysicalAttack)},
	}
}

/*
================
applyShieldDefensePenalty

Inferred: shield physical defense includes the current shield's base,
variance, plus and STR reinforcement. Apply its percentage cut on the
flat defense lane before any total-defense percentage scaling. This
keeps armor, magical defense and other buffs outside the shield penalty.
The detached graph is reconstructed after every equipment/stat change.
================
*/
func applyShieldDefensePenalty(graph *paramkeeper.Graph, base float32) error {
	percent, err := graph.Value(shieldDefensePenaltyParameter)
	if err != nil || percent == 0 {
		return err
	}
	strength, err := graph.Value(1)
	if err != nil {
		return err
	}
	reinforcement, err := graph.Value(physicalShieldReinforcement)
	if err != nil {
		return err
	}
	defense := float64(base) + float64(strength)*float64(reinforcement)/shieldPenaltyPercent
	penalty := float32(-defense * float64(percent) / shieldPenaltyPercent)
	_, err = graph.Apply(attributePhysicalDefense, paramkeeper.Flat,
		shieldDefensePenaltySource, penalty)
	return err
}
