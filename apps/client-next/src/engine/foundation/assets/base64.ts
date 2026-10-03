/*
===========================================================================

base64.ts - decode a published base64 column into bytes

Navigation grids, object meshes and terrain columns are published as
base64 text. They were decoded with Uint8Array.from( atob( text ), c =>
c.charCodeAt( 0 ) ), which makes a one-character string for every byte:
a region install spent most of an 87 ms simulation stall there. The
native decoder runs where the browser has it; elsewhere a charCodeAt loop
produces the same bytes without per-byte strings.

===========================================================================
*/

interface NativeBase64 {
	fromBase64?( text: string ): Uint8Array;
}

/*
================
base64Bytes

The bytes a standard base64 string encodes. Throws on invalid text, as
atob does.
================
*/
export function base64Bytes( text: string ): Uint8Array {
	const native = (Uint8Array as unknown as NativeBase64).fromBase64;
	if ( native ) return native.call( Uint8Array, text );
	const raw = atob( text ), bytes = new Uint8Array( raw.length );
	for ( let i = 0; i < raw.length; i++ ) bytes[i] = raw.charCodeAt( i );
	return bytes;
}
