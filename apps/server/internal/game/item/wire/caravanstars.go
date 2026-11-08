/*
===========================================================================

caravanstars.go - the 5-star caravan roster push (port-only, not native)

Under SRO_TRADE_TIERS (#2958) a 5-star caravan is a target: its star
shows on the transport's nameplate for everyone nearby. The GameWorld
pushes the division's 5-star transport gids with this frame; only this
port's browser client consumes it, and a client that predates it drops
the unknown opcode, so no release-protocol contract carries it. 0x3FBB
is a port-only opcode: no v1.150 client or v1.188 server uses it.

Body, little-endian:

	u16 count
	count x { u32 gid }

===========================================================================
*/

package wire

import (
	"encoding/binary"
)

// OpCaravanStars is the port-only S->C 5-star caravan roster push.
const OpCaravanStars uint16 = 0x3FBB

// CaravanStarsMax bounds one frame; the rest are left out.
const CaravanStarsMax = 1024

/*
================
EncodeCaravanStars
================
*/
func EncodeCaravanStars(gids []uint32) []byte {
	if len(gids) > CaravanStarsMax {
		gids = gids[:CaravanStarsMax]
	}
	out := binary.LittleEndian.AppendUint16(nil, uint16(len(gids)))
	for _, gid := range gids {
		out = binary.LittleEndian.AppendUint32(out, gid)
	}
	return out
}
