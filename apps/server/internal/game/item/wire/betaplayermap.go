/*
===========================================================================

betaplayermap.go - the beta world map roster push (port-only, not native)

The retail world map shows only the local player and party members. For
the public beta the operator can also show every online player; the
server pushes their positions with this frame. 0x3FB0 is a port-only
opcode: no v1.150 client or v1.188 server uses it, and only this port's
browser client consumes it.

Body, little-endian:

	u16 count
	count x { u32 gid, u16 region, f32 x, f32 z, u8 nameLength, name bytes }

===========================================================================
*/

package wire

import (
	"encoding/binary"
	"math"
)

// OpBetaPlayerMap is the port-only S->C world map roster push.
const OpBetaPlayerMap uint16 = 0x3FB0

// BetaPlayerMapMaxPlayers bounds one frame; the rest are left out.
const BetaPlayerMapMaxPlayers = 1024

// betaPlayerMapMaxName bounds a name's bytes (a u8 length).
const betaPlayerMapMaxName = 64

// BetaMapPlayer is one roster row: a player's gid, region and region-local
// ground position.
type BetaMapPlayer struct {
	Gid      uint32
	RegionID uint16
	X, Z     float32
	Name     string
}

/*
================
EncodeBetaPlayerMap
================
*/
func EncodeBetaPlayerMap(players []BetaMapPlayer) []byte {
	if len(players) > BetaPlayerMapMaxPlayers {
		players = players[:BetaPlayerMapMaxPlayers]
	}
	out := binary.LittleEndian.AppendUint16(nil, uint16(len(players)))
	for _, p := range players {
		name := p.Name
		if len(name) > betaPlayerMapMaxName {
			name = name[:betaPlayerMapMaxName]
		}
		out = binary.LittleEndian.AppendUint32(out, p.Gid)
		out = binary.LittleEndian.AppendUint16(out, p.RegionID)
		out = binary.LittleEndian.AppendUint32(out, math.Float32bits(p.X))
		out = binary.LittleEndian.AppendUint32(out, math.Float32bits(p.Z))
		out = append(out, uint8(len(name)))
		out = append(out, name...)
	}
	return out
}
