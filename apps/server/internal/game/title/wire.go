/*
===========================================================================

wire.go - the peer title frame (later-native titles, #3502 slice 1)

OpPeerTitle is an S->C opcode of this build, the next free one after
the contributor prompt (0x3FC2); server wire and client dispatch were
both searched for 0x3FC3. The later native client keeps the title id
on the character object and prints it on the name line (v1.696 client,
pinned sha256 52118aaf075877851483d400b6b2905943e47b97b971d85b6cf59c3032c32d34:
the overhead name builder 0xF5F7C0 reads the id at object+0x88
(0xF5F92D), resolves it through the by-id RefTitle lookup 0x907190
and prints the record text at +0x28 through the title's format; the
chat renderer 0xCEB3A0 formats the same text as <%s>%s). Which
spawn-stream read stores the id there was not isolated in that build
(searched: every caller of 0x907190, and the census of dword writes
to +0x88), so no native byte position is claimed for it. The port
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

/*
================
EncodePeerTitle

One peer title frame body for a spawned player. The caller sends it
only for a worn title (id non-zero); the format resolves here, empty
when the table does not carry the id. The u16 length always holds it:
ParseTitles refuses a format over maxFormatBytes.
================
*/
func EncodePeerTitle(gid, titleID uint32) []byte {
	// ParseTitles admitted only bounded, valid UTF-8 formats, so the
	// format goes out whole: cutting it here could split a character,
	// and the client decodes the bytes strictly.
	format, _ := FormatFor(titleID)
	out := binary.LittleEndian.AppendUint32(nil, gid)
	out = binary.LittleEndian.AppendUint32(out, titleID)
	out = binary.LittleEndian.AppendUint16(out, uint16(len(format)))
	return append(out, format...)
}
