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

// HwanLevelMax is the highest Blue Berserk level the level table holds
// (_RefHWANLevel rows 0 to 6).
const HwanLevelMax = 6

// SetHwanLevel raises the Blue Berserk level to level (clamped to
// HwanLevelMax) and reports whether it changed. A quest never lowers a
// level a character already holds. Call only inside the character store
// mutation door.
func (c *Character) SetHwanLevel(level uint8) bool {
	level = min(level, HwanLevelMax)
	if level <= c.HwanLevel {
		return false
	}
	c.HwanLevel = level
	return true
}
