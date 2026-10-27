package monster

// FollowLocationCompatible models only the region-word predicate in the
// reference server's sector-adjacency helper. Outdoor sectors are adjacent
// in each unsigned byte axis;
// they do not wrap. Both dungeon-tagged words pass this native helper, but
// that does NOT establish shared dungeon-instance identity or navigation.
func FollowLocationCompatible(left, right uint16) bool {
	if left>>15 != right>>15 {
		return false
	}
	if left>>15 != 0 {
		return true
	}
	dx := int(uint8(left)) - int(uint8(right))
	dz := int(uint8(left>>8)) - int(uint8(right>>8))
	return dx >= -1 && dx <= 1 && dz >= -1 && dz <= 1
}
