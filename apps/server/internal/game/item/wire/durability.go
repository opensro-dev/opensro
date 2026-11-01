package wire

import "encoding/binary"

// OpItemDurability is the v1.150 registration at client 74D916 -> 77C300.
// The research server's equivalent uses 3052 in that different build.
const OpItemDurability uint16 = 0x31e8

// ItemDurabilityFrame is private to the item's owner. Parameter/passive refresh
// on a zero crossing precedes this frame (the research server's ordering).
// The uint32 retains
// the exact bits consumed as a signed comparison by the native client.
func ItemDurabilityFrame(slot uint8, value uint32) Frame {
	p := make([]byte, 5)
	p[0] = slot
	binary.LittleEndian.PutUint32(p[1:], value)
	return Frame{Opcode: OpItemDurability, Payload: p}
}
