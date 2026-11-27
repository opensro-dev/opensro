/*
===========================================================================

census-reads.mjs - what the action census sees of a published file

The census fixture (ui-action-census.mjs) hands the UI the bytes of a
JSON, text or texture file, but of a PNG only its size: the image is
never decoded. The census gate (ui-action-census-gate.mjs) keys each
scene by the files it read, so a PNG is keyed by that size, not by its
bytes: of the 1,286 files every scene reads, 1,097 are PNGs, and a
reskin that keeps their sizes leaves every scene's census alone (#4337).

===========================================================================
*/
import { closeSync, openSync, readSync } from "node:fs";

// The IHDR chunk's width and height, big-endian after the 8-byte
// signature and the chunk's length and type (PNG specification 11.2.2).
const PNG_WIDTH_OFFSET = 16;
const PNG_HEIGHT_OFFSET = 20;
const PNG_SIZE_BYTES = 24;

/*
================
pngSize

The width and height the fixture reports for a PNG's bytes; throws on
a file too short to hold them, as a read failure.
================
*/
export function pngSize( bytes ) {
	return { width: bytes.readUInt32BE( PNG_WIDTH_OFFSET ), height: bytes.readUInt32BE( PNG_HEIGHT_OFFSET ) };
}

/*
================
pngSizeDigest

The census digest of a PNG on disk: its size, read from the first
PNG_SIZE_BYTES only. Throws when the file cannot be read or is short.
================
*/
export function pngSizeDigest( file ) {
	const bytes = Buffer.alloc( PNG_SIZE_BYTES ), handle = openSync( file, "r" );
	try {
		const read = readSync( handle, bytes, 0, PNG_SIZE_BYTES, 0 );
		const { width, height } = pngSize( bytes.subarray( 0, read ) );
		return `png ${width}x${height}`;
	} finally {
		closeSync( handle );
	}
}
