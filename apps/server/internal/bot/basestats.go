/*
===========================================================================

basestats.go - the maximum HP from the base-stat block

The server sends the 0x343C base-stat block to the character itself when a
derived stat changes: at a level-up, at a stat allocation and at an equipment
change. The block carries the real maximum HP. The vitals refresh carries
only the current HP, so this block is the source of the potion threshold
(issue #140).

===========================================================================
*/
package bot

import (
	"encoding/binary"

	"opensro.online/server/internal/game/item/wire"
)

// OpcodeBaseStats is the 0x343C base-stat block.
const OpcodeBaseStats uint16 = wire.OpBaseStats

// baseStatsMaxHPOffset is the offset of the u32 maximum HP in the block: four
// u32 attack fields and four u16 defence fields come first.
const baseStatsMaxHPOffset = 24

// ParseBaseStatsMaxHP reads the maximum HP from a 0x343C body. It rejects a
// short body and a zero maximum.
func ParseBaseStatsMaxHP(payload []byte) (int64, bool) {
	if len(payload) < wire.BaseStatsSize {
		return 0, false
	}
	maxHP := binary.LittleEndian.Uint32(payload[baseStatsMaxHPOffset : baseStatsMaxHPOffset+4])
	if maxHP == 0 {
		return 0, false
	}
	return int64(maxHP), true
}
