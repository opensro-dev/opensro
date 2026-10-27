package combat

import "math"

// The server's knockdown formula: rank-scaled chance, spilled to float32, clamped to 0..75,
// truncated to a byte before the caster-owned probability ledger.
func KnockdownChance(rank, chance uint32, level uint8) uint8 {
	value := float32(float64(rank) * 2 / float64(rank+uint32(level)) * float64(chance))
	if math.IsNaN(float64(value)) || value <= 0 {
		return 0
	}
	if value > 75 {
		value = 75
	}
	return uint8(value)
}
