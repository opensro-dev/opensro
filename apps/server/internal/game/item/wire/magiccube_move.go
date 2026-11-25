/*
===========================================================================

magiccube_move.go - Magic Cube 0x706D push/pop result bodies (#3906)

Port-only movement types 0x27/0x28: success payloads the action lane
writes after a deposit or withdraw. Request encode/decode stay in
itemmove.go beside the other movement types.

===========================================================================
*/
package wire

// EncodePushIntoMagicCubeResult returns a successful type-0x27 payload:
// [0x01][0x27][src][dst][qty u16]. v1.696 client body reader at client 0x7f7be0.
/*
================
EncodePushIntoMagicCubeResult
================
*/
func EncodePushIntoMagicCubeResult(sourceSlot, destSlot uint8, quantity uint16) []byte {
	return NewWriter(6).
		U8(ResultSuccess).
		U8(MoveTypePushIntoMagicCube).
		U8(sourceSlot).
		U8(destSlot).
		U16(quantity).
		Payload()
}

// EncodePopFromMagicCubeResult returns a successful type-0x28 payload:
// [0x01][0x28][u32 RefObjID][src][dst][qty u16]. v1.696 client body reader at client 0x7f7b90.
/*
================
EncodePopFromMagicCubeResult
================
*/
func EncodePopFromMagicCubeResult(refObjID uint32, sourceSlot, destSlot uint8, quantity uint16) []byte {
	return NewWriter(10).
		U8(ResultSuccess).
		U8(MoveTypePopFromMagicCube).
		U32(refObjID).
		U8(sourceSlot).
		U8(destSlot).
		U16(quantity).
		Payload()
}
