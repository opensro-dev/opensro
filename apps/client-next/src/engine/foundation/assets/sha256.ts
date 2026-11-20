/*
===========================================================================

sha256.ts - the lowercase hex SHA-256 of a byte buffer

Content-addressed files (asset packs, transport references) are named and
checked by this one digest form. The asset and simulation workers share it,
so a name and its check can never disagree on case or padding.

===========================================================================
*/

/*
================
sha256Hex
================
*/
export async function sha256Hex( bytes: Uint8Array<ArrayBuffer> ): Promise<string> {
	return Array.from(
		new Uint8Array( await crypto.subtle.digest( "SHA-256", bytes ) ),
		byte => byte.toString( 16 ).padStart( 2, "0" )
	).join( "" );
}
