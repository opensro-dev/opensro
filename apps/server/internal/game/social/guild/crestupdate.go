/*
===========================================================================

crestupdate.go - the 0x34F3 crest update rows

When a crest changes, the clients that hold the guild or union restate
it from this frame: type 1 carries one guild's crest, type 2 one
union's crest with the name of every guild standing in it. A crest of
zero is a crest deleted; 0xFFFFFFFF is the client's no-change mark and
is never sent from here.

===========================================================================
*/
package guild

import (
	"opensro.online/server/internal/game/item/wire"
)

/*
================
EncodeGuildCrestUpdate

The type-1 row: {u8 1}{u32 guild id}{str guild name}{u32 crest}.
================
*/
func EncodeGuildCrestUpdate(guildID uint32, guildName string, crest uint32) []byte {
	writer := wire.NewWriter(16 + len(guildName))
	writer.U8(1).U32(guildID)
	writeGuildString(writer, guildName)
	writer.U32(crest)
	return writer.Payload()
}

/*
================
EncodeAllianceCrestUpdate

The type-2 row: {u8 2}{u32 union id}{u8 name count}{str names}{u32 crest}.
================
*/
func EncodeAllianceCrestUpdate(allianceID uint32, guildNames []string, crest uint32) []byte {
	size := 8
	for _, name := range guildNames {
		size += 2 + len(name)
	}
	writer := wire.NewWriter(size)
	writer.U8(2).U32(allianceID).U8(uint8(len(guildNames)))
	for _, name := range guildNames {
		writeGuildString(writer, name)
	}
	writer.U32(crest)
	return writer.Payload()
}
