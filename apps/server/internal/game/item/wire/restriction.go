package wire

// OpGMRestrictionNotice is v1.150 74BFF0's registered 36EA carrier.
const OpGMRestrictionNotice uint16 = 0x36EA

// EncodeGMRestrictionNotice uses the server's packed SYSTEMTIME layout.
// kind is the notice selector (0 chat, 1 trade), not the command source's
// record ID (4/3).
// Native masks fields rather than validating or converting timezone/date.
func EncodeGMRestrictionNotice(kind uint8, date [8]uint16) []byte {
	packed := uint32(date[6])
	packed = packed<<6 | uint32(date[5]&63)
	packed = packed<<5 | uint32(date[4]&31)
	packed = packed<<5 | uint32(date[3]&31)
	packed = packed<<4 | uint32(date[1]&15)
	packed = packed<<6 | uint32((date[0]-16)&63)
	return NewWriter(5).U8(kind).U32(packed).Payload()
}
