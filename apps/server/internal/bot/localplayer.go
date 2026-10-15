/*
===========================================================================

localplayer.go - the bot's own start position from the char-data packet

The server sends the local player's own char-data (0x32B3) at enter-world. The
payload ends its runtime block with the object id, the region and the position
(enterworld.BuildLocalPlayerEntryPayload). The bot reads its own position from
that block, so it knows where it stands before the first move.

Phase 5 of issue #74 (issue #117).

===========================================================================
*/
package bot

import (
	"bytes"
	"encoding/binary"
	"math"
)

// maxRegionLocal bounds a region-local coordinate. A real region is 1920 or
// 3200 world units, so a value above this bound is not a position and the
// match is a false positive.
const maxRegionLocal = 4096

// ParseCharacterPosition reads the local player's own region and position from
// the 0x32B3 char-data payload. The block is the object id, the region (u16),
// x, y, z (f32 each) and the angle (u16). The search runs from the end, so a
// later block wins over an earlier object-id match. A non-zero region pins the
// block to the region the reset-client packet announced, and the position 0,0
// is rejected, so a false object-id match cannot win.
func ParseCharacterPosition(payload []byte, gid uint32, region uint16) (uint16, float64, float64, float64, bool) {
	if gid == 0 || len(payload) < 18 {
		return 0, 0, 0, 0, false
	}
	var needle [4]byte
	binary.LittleEndian.PutUint32(needle[:], gid)
	for at := len(payload) - 4; at >= 0; at-- {
		if !bytes.Equal(payload[at:at+4], needle[:]) || len(payload)-at < 18 {
			continue
		}
		found := binary.LittleEndian.Uint16(payload[at+4 : at+6])
		x := math.Float32frombits(binary.LittleEndian.Uint32(payload[at+6 : at+10]))
		y := math.Float32frombits(binary.LittleEndian.Uint32(payload[at+10 : at+14]))
		z := math.Float32frombits(binary.LittleEndian.Uint32(payload[at+14 : at+18]))
		if found == 0 || !plausibleCoord(x) || !plausibleCoord(z) || (x == 0 && z == 0) {
			continue
		}
		if region != 0 && found != region {
			continue
		}
		return found, float64(x), float64(y), float64(z), true
	}
	return 0, 0, 0, 0, false
}

// plausibleCoord reports whether value is a finite region-local coordinate.
func plausibleCoord(value float32) bool {
	f := float64(value)
	return !math.IsNaN(f) && !math.IsInf(f, 0) && f >= 0 && f < maxRegionLocal
}
