package world

// MessageBlock identifies a native activity bucket. World/partition identity
// belongs to its caller. Dungeon grid coordinates are relative to the DOF
// navigation bounds, not outdoor region coordinates (the reference
// server's message-block bucketing uses the same bounds).
type MessageBlock struct {
	Region  uint16 // zero outdoors, where X/Z already include region coordinates
	X, Y, Z int
}

// OutdoorMessageBlock is the message block containing an outdoor
// position, ok=false for dungeon positions (the dungeon branch is
// InterestBlockAt's own, in the plane owner geometry.go, #1646).
func OutdoorMessageBlock(position RegionXZ) (MessageBlock, bool) {
	b := InterestBlockAt(position)
	if b.Dungeon != 0 {
		return MessageBlock{}, false
	}
	return MessageBlock{X: b.X, Z: b.Z}, true
}

func (b MessageBlock) Neighbors() []MessageBlock {
	out := make([]MessageBlock, 0, 27)
	b.NeighborsEach(func(n MessageBlock) {
		out = append(out, n)
	})
	return out
}

/*
================
MessageBlock.NeighborsEach

NeighborsEach visits the same blocks Neighbors returns, in the same
order, without allocating the slice: the activity census counts one
bucket per neighbour per session per leg and keeps none of them.
================
*/
func (b MessageBlock) NeighborsEach(visit func(MessageBlock)) {
	yMin, yMax := 0, 0
	if b.Region != 0 {
		yMin, yMax = -1, 1
	}
	for x := -1; x <= 1; x++ {
		for y := yMin; y <= yMax; y++ {
			for z := -1; z <= 1; z++ {
				visit(MessageBlock{Region: b.Region, X: b.X + x, Y: b.Y + y, Z: b.Z + z})
			}
		}
	}
}
