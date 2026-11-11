/*
===========================================================================

tradeprogress.go - the caravan's distance to the next star (port-only, not native)

Under SRO_TRADE_TIERS (#3163) the trader cannot tell how a star was
earned or how close the next one is. With the star answer (0x352D)
the server also publishes the cargo's star count and the cargo gold
still missing from the next star (action.TradeGoldGap, the same tier
function that rates the cargo), so the client can name the distance
in stacks of the goods in front of the player and the two cannot
disagree. Only this port's browser client consumes it, and a client
that predates it drops the unknown opcode, so no release-protocol
contract carries it. 0x3FC0 is a port-only opcode of this build, the
next unallocated neighbour past the port's own 0x3FB0..0x3FBF frames
(server wire and client dispatch both checked, #3163).

Body, little-endian:

	u8 stars
	u32 gold missing from the next star (0 at five stars)

===========================================================================
*/

package wire

import (
	"encoding/binary"
	"math"
)

// OpTradeProgress is the port-only S->C trade-tier progress answer.
const OpTradeProgress uint16 = 0x3FC0

/*
================
EncodeTradeProgress
================
*/
func EncodeTradeProgress(stars uint8, gap uint64) []byte {
	if gap > math.MaxUint32 {
		gap = math.MaxUint32
	}
	out := []byte{stars}
	return binary.LittleEndian.AppendUint32(out, uint32(gap))
}
