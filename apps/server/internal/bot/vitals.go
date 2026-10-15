/*
===========================================================================

vitals.go - the bot's HP and MP from the vitals refresh

The server sends the 0x33a6 vitals refresh after item use and on damage.
The brain uses the current HP to decide when to drink a potion.

Phase 2 of issue #74 (issue #76).

===========================================================================
*/
package bot

import "encoding/binary"

// OpcodeVitalsUpdate is the 0x33a6 HP/MP refresh.
const OpcodeVitalsUpdate uint16 = 0x33a6

// Vitals is the current HP and MP of one entity. GID is the entity the
// refresh belongs to; the server sends the refresh for the bot's own
// character, so the bot learns its object id here.
type Vitals struct {
	GID uint32
	HP  int64
	MP  int64
}

// ParseVitals parses the 0x33a6 body:
// [u32 objectID][u16 0][u8 0x03][u32 hp][u32 mp].
func ParseVitals(payload []byte) (Vitals, bool) {
	if len(payload) < 15 {
		return Vitals{}, false
	}
	return Vitals{
		GID: binary.LittleEndian.Uint32(payload[0:4]),
		HP:  int64(binary.LittleEndian.Uint32(payload[7:11])),
		MP:  int64(binary.LittleEndian.Uint32(payload[11:15])),
	}, true
}

// OpcodeItemUseRequest is the 0x75BD selected-item use request. The common
// potion body is [u8 slot][u16 type word].
const OpcodeItemUseRequest uint16 = 0x75BD

// EncodeItemUse builds the 0x75BD potion body.
func EncodeItemUse(slot uint8, typeWord uint16) []byte {
	body := make([]byte, 3)
	body[0] = slot
	binary.LittleEndian.PutUint16(body[1:3], typeWord)
	return body
}
