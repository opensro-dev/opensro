package combat

import "opensro.online/server/internal/game/enterworld"

func downAttackDamage(damage uint32, motionState uint8, modifier enterworld.SkillDownAttack) uint32 {
	if !modifier.Present || motionState != 8 {
		return damage
	}
	// The server's down-attack IMUL keeps the low dword BEFORE unsigned conversion
	// and division by 100. Widening the product changes overflow behavior.
	return (damage * modifier.Percent) / 100
}
