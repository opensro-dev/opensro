/*
===========================================================================

hash.mjs - SHA-256 helpers of the asset pipeline

The digests content addressing, manifests and input checks share.

===========================================================================
*/
import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";

/*
================
sha256Hex

One-shot SHA-256 used by generated-asset manifests and content-addressed outputs.
================
*/
export function sha256Hex( value ) {
	return createHash( "sha256" ).update( value ).digest( "hex" );
}

/*
================
sha256File

Streaming SHA-256 of a file, for inputs too large to read at once (client archives).
================
*/
export function sha256File( file ) {
	return new Promise( ( resolve, reject ) => {
		const hash = createHash( "sha256" );
		createReadStream( file )
			.on( "data", ( chunk ) => hash.update( chunk ) )
			.on( "error", reject )
			.on( "end", () => resolve( hash.digest( "hex" ) ) );
	} );
}
