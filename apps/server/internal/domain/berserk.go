package domain

// ModifyBerserkPoints is the persistent point owner (as the native player's
// berserk-point routine). Call only inside the character store mutation
// door.
func (c *Character) ModifyBerserkPoints(delta int) bool {
	if delta == 0 || delta > 0 && c.NativeBodyStatus == 1 {
		return false
	}
	next := uint8(max(0, min(5, int(c.BerserkPoints)+delta)))
	if next == c.BerserkPoints {
		return false
	}
	c.BerserkPoints = next
	return true
}
