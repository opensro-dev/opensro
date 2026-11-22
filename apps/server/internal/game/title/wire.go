/*
===========================================================================

wire.go - the peer title frame (later-native titles, #3502 slice 1)

OpPeerTitle is an S->C opcode of this build, the next free one after
the contributor prompt (0x3FC2); server wire and client dispatch were
both searched for 0x3FC3. The later native client reads the title id
off the spawn stream (its spawn reader at 0x96DA40 reads a u32 and
calls its title setter, then builds the display name). The port
carries the id here instead, because a tab loaded before this release
decodes the player spawn row length-strictly: growing the row would
fail old tabs at the first player spawn, while a client that predates
this frame drops the unknown opcode (the contributor roster's rule,
0x3FC1). The frame rides right after the peer's spawn row, and on its
own when a title changes.

Body, little-endian:

	u32 gid, u32 title id, u16 format length, format bytes (UTF-8)

The format is the server's resolution of the id (title.go), so a tab
needs no title table of its own to print a name line. An id the table
does not know carries an empty format and prints the plain name, as a
native title lookup miss does. Only this port's browser client
consumes the frame.

===========================================================================
*/
package title

import "encoding/binary"

// OpPeerTitle is the port-carried S->C peer title push.
const OpPeerTitle uint16 = 0x3FC3

// maxFormatBytes bounds the format a frame carries; the later client's
// formats are a few dozen bytes, so a longer one is a bad table row,
// not a title.
const maxFormatBytes = 512

/*
================
EncodePeerTitle

One peer title frame body for a spawned player. The caller sends it
only for a worn title (id non-zero); the format resolves here, empty
when the table does not carry the id.
================
*/
func EncodePeerTitle(gid, titleID uint32) []byte {
	format, _ := FormatFor(titleID)
	if len(format) > maxFormatBytes {
		format = format[:maxFormatBytes]
	}
	out := binary.LittleEndian.AppendUint32(nil, gid)
	out = binary.LittleEndian.AppendUint32(out, titleID)
	out = binary.LittleEndian.AppendUint16(out, uint16(len(format)))
	return append(out, format...)
}
