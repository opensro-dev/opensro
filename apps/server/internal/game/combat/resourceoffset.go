package combat

// OffsetVital follows the server's HP/MP offset routines: ADD wraps at 32 bits, then the
// resulting signed value is clamped to zero and the signed maximum.
func OffsetVital(current, maximum uint32, delta int32) uint32 {
	value := int32(current + uint32(delta))
	if value < 0 {
		value = 0
	}
	if value > int32(maximum) {
		value = int32(maximum)
	}
	return uint32(value)
}

// ConsumeMana uses the nonnegative cost the server's mana spend takes, rather than allowing
// an overflowing prepared cost to become a mana grant.
func ConsumeMana(current, maximum uint32, cost int32) uint32 {
	if cost <= 0 {
		return current
	}
	return OffsetVital(current, maximum, -cost)
}
