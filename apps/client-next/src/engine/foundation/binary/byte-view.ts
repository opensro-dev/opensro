/*
===========================================================================

byte-view.ts - a DataView over exactly the bytes a typed array holds

Wire payloads and asset records arrive as Uint8Array slices of a larger
buffer. A DataView built from the slice's buffer alone would read from
byte 0 of that buffer, not from the slice; every decoder therefore needs
the buffer, the offset and the length together. This file is the one
place that spells them out.

===========================================================================
*/

/*
================
viewOf

A DataView whose byte 0 is bytes[0] and whose length is bytes.byteLength.
================
*/
export function viewOf( bytes: ArrayBufferView ): DataView {
	return new DataView( bytes.buffer, bytes.byteOffset, bytes.byteLength );
}
