/*
===========================================================================

level.go - the bot's own level

The server sends the level in the local player char-data (0x32B3, the byte
after the model ref and the body shape) and a level-up presentation (0x36B0)
with the object id. The bot tracks its level, so a level goal can stop the
hunt.

Phase 5 of issue #74 (issue #130).

===========================================================================
*/
package bot

import (
	"encoding/binary"

	"opensro.online/server/internal/game/item/wire"
)

// OpcodeLevelUpEffect is the 0x36B0 level-up presentation. The payload is the
// object id.
const OpcodeLevelUpEffect uint16 = wire.OpLevelUpEffect

// LevelUpGID returns the gid of a 0x36B0 payload.
func LevelUpGID(payload []byte) (uint32, bool) {
	if len(payload) < 4 {
		return 0, false
	}
	return binary.LittleEndian.Uint32(payload[:4]), true
}

// ParseCharacterLevel reads the local player's own level from the 0x32B3
// char-data payload. The level is the byte after the model ref (u32) and the
// body shape (u8).
func ParseCharacterLevel(payload []byte) (int, bool) {
	if len(payload) < 6 {
		return 0, false
	}
	level := int(payload[5])
	if level < 1 || level > 140 {
		return 0, false
	}
	return level, true
}
